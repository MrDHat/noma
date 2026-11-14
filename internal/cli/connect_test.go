package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveConfigIsPrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "noma", "config.json")
	t.Setenv("NOMA_CONFIG", path)
	want := &Config{Token: "t", PortForward: &PortForward{Context: "c", Namespace: "n", Service: "s", Port: 1}}
	if err := saveConfig(path, want); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Errorf("config mode %v, want 0600", st.Mode().Perm())
	}
	got, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if got.Token != want.Token || *got.PortForward != *want.PortForward {
		t.Errorf("read back %+v, want %+v", got, want)
	}
}
