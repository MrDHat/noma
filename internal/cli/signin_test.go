package cli

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"
)

func TestLinesKeepsLineForNextPrompt(t *testing.T) {
	r, w := io.Pipe()
	l := newLines(r)

	// A prompt given up on before the user typed anything...
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := l.read(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("read with no input = %v, want deadline exceeded", err)
	}
	// ...leaves the next line to the next prompt.
	go func() {
		_, _ = io.WriteString(w, "fake-code-1\n")
		_ = w.Close()
	}()
	if got, err := l.read(context.Background()); err != nil || got != "fake-code-1" {
		t.Fatalf("read = %q, %v; want the line", got, err)
	}
	for range 2 {
		if _, err := l.read(context.Background()); !errors.Is(err, io.EOF) {
			t.Fatalf("read after the end = %v, want EOF", err)
		}
	}
}
