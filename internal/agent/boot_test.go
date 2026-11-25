package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCountBootGoesUp(t *testing.T) {
	dir := t.TempDir()
	for want := int64(1); want <= 3; want++ {
		got, err := countBoot(dir)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("boot %d, want %d", got, want)
		}
	}
}

func TestCountBootRefusesAGarbledCount(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, bootFile), []byte("x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := countBoot(dir); err == nil {
		t.Fatal("countBoot accepted a garbled count")
	}
}
