package secrets

import (
	"bytes"
	"encoding/base64"
	"testing"
)

func TestSealOpen(t *testing.T) {
	k, err := ParseKey("k1", base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32)))
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := k.Seal([]byte("ghp_secret"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, []byte("ghp_secret")) {
		t.Fatal("sealed value contains the plaintext")
	}
	if got, err := k.Open("k1", sealed); err != nil || string(got) != "ghp_secret" {
		t.Fatalf("Open = %q, %v", got, err)
	}
	if _, err := k.Open("k0", sealed); err == nil {
		t.Error("opened a value sealed under another key ID")
	}
	sealed[len(sealed)-1] ^= 1
	if _, err := k.Open("k1", sealed); err == nil {
		t.Error("opened a tampered value")
	}
	for _, bad := range []string{"", "not base64", base64.StdEncoding.EncodeToString([]byte("short"))} {
		if _, err := ParseKey("k1", bad); err == nil {
			t.Errorf("ParseKey(%q) succeeded", bad)
		}
	}
}
