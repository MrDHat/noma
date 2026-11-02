package cli

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"golang.org/x/crypto/nacl/box"

	"github.com/MrDHat/noma/internal/api"
)

// signIn walks the user through one Claude sign-in: it opens the URL, asks
// for the code and sends it sealed to the workspace's one-time key, so
// noma-server only ever handles ciphertext.
func signIn(ctx context.Context, c *Client, name string, s *api.SignIn, in *bufio.Reader, out io.Writer) error {
	key, err := base64.StdEncoding.DecodeString(s.PublicKey)
	if err != nil || len(key) != 32 {
		return errors.New("workspace sent an invalid sign-in key")
	}
	fmt.Fprintf(out, "\nSign in to Claude for this workspace. Opening your browser:\n  %s\n", s.URL)
	openBrowser(s.URL)
	fmt.Fprint(out, "After approving, paste the code here.\nPaste code: ")
	code, err := readLine(ctx, in)
	if err != nil {
		return err
	}
	if code == "" {
		return errors.New("no code entered")
	}
	sealed, err := box.SealAnonymous(nil, []byte(code), (*[32]byte)(key), rand.Reader)
	if err != nil {
		return err
	}
	return c.SendSignInCode(ctx, name, api.SignInCode{
		PublicKey:  s.PublicKey,
		SealedCode: base64.StdEncoding.EncodeToString(sealed),
	})
}

// readLine reads one line, giving up when ctx is done.
func readLine(ctx context.Context, in *bufio.Reader) (string, error) {
	type result struct {
		line string
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		line, err := in.ReadString('\n')
		if errors.Is(err, io.EOF) && line != "" {
			err = nil
		}
		ch <- result{strings.TrimSpace(line), err}
	}()
	select {
	case r := <-ch:
		return r.line, r.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// openBrowser opens url in the user's browser, if it can. The URL is
// printed too, so failing quietly is fine.
func openBrowser(url string) {
	if os.Getenv("NOMA_NO_BROWSER") != "" {
		return
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	default:
		return
	}
	_ = cmd.Start()
}
