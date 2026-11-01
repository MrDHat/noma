package agent

import (
	"os"
	"os/exec"
	"os/signal"
	"syscall"
)

// childEnv marks the re-executed agent process.
const childEnv = "NOMA_AGENT_CHILD=1"

// RunAsInit makes the agent a proper PID 1. It re-runs itself as a child and
// spends its life reaping orphaned processes, which would otherwise pile up
// as zombies (a dead claude left as a zombie blocks a new remote-control
// server). Signals are passed to the child. It reports false when the agent
// is not PID 1 or is already the child, and the caller should just run.
func RunAsInit() (exitCode int, handled bool) {
	if os.Getpid() != 1 || os.Getenv("NOMA_AGENT_CHILD") == "1" {
		return 0, false
	}
	self, err := os.Executable()
	if err != nil {
		return 1, true
	}
	cmd := exec.Command(self, os.Args[1:]...)
	cmd.Env = append(os.Environ(), childEnv)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr

	sigs := make(chan os.Signal, 8)
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP, syscall.SIGCHLD)
	if err := cmd.Start(); err != nil {
		return 1, true
	}
	child := cmd.Process.Pid
	for sig := range sigs {
		if sig != syscall.SIGCHLD {
			_ = cmd.Process.Signal(sig)
			continue
		}
		for {
			var status syscall.WaitStatus
			pid, err := syscall.Wait4(-1, &status, syscall.WNOHANG, nil)
			if err != nil || pid <= 0 {
				break
			}
			if pid == child {
				return status.ExitStatus(), true
			}
		}
	}
	return 0, true
}
