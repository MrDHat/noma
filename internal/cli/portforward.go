package cli

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"time"
)

var forwardingLine = regexp.MustCompile(`Forwarding from 127\.0\.0\.1:(\d+)`)

// startPortForward runs kubectl port-forward to a free local port. It returns
// the server URL and a function that stops the forward.
func startPortForward(ctx context.Context, pf *PortForward) (string, func(), error) {
	cmd := exec.CommandContext(ctx, "kubectl", "--context", pf.Context, "-n", pf.Namespace,
		"port-forward", "--address", "127.0.0.1", "svc/"+pf.Service, fmt.Sprintf(":%d", pf.Port))
	out, err := cmd.StdoutPipe()
	if err != nil {
		return "", nil, err
	}
	if err := cmd.Start(); err != nil {
		return "", nil, fmt.Errorf("start kubectl port-forward: %w", err)
	}
	stop := func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}

	found := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(out)
		for sc.Scan() {
			if m := forwardingLine.FindStringSubmatch(sc.Text()); m != nil {
				found <- m[1]
				break
			}
		}
		for sc.Scan() { // keep draining so kubectl never blocks on stdout
		}
	}()
	select {
	case port := <-found:
		return "http://127.0.0.1:" + port, stop, nil
	case <-time.After(15 * time.Second):
		stop()
		return "", nil, fmt.Errorf("kubectl port-forward to %s/%s did not start", pf.Namespace, pf.Service)
	}
}
