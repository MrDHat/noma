// Package secrets encrypts credential values at rest with NaCl secretbox.
package secrets

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"

	"golang.org/x/crypto/nacl/secretbox"
)

// Key is the server's secret key and the ID stored with every value it
// seals, so keys can be rotated.
type Key struct {
	ID  string
	key [32]byte
}

// ParseKey reads a base64-encoded 32-byte key.
func ParseKey(id, b64 string) (*Key, error) {
	if id == "" {
		return nil, errors.New("secret key has no ID")
	}
	b, err := base64.StdEncoding.DecodeString(b64)
	if err != nil || len(b) != 32 {
		return nil, errors.New("secret key must be 32 bytes, base64-encoded")
	}
	k := &Key{ID: id}
	copy(k.key[:], b)
	return k, nil
}

// Seal encrypts value under a fresh random nonce, which it prepends.
func (k *Key) Seal(value []byte) ([]byte, error) {
	var nonce [24]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, err
	}
	return secretbox.Seal(nonce[:], value, &nonce, &k.key), nil
}

// Open decrypts what Seal returned under the key with keyID.
func (k *Key) Open(keyID string, sealed []byte) ([]byte, error) {
	if keyID != k.ID {
		return nil, fmt.Errorf("value is sealed with key %q, not %q", keyID, k.ID)
	}
	if len(sealed) < 24 {
		return nil, errors.New("sealed value too short")
	}
	var nonce [24]byte
	copy(nonce[:], sealed)
	value, ok := secretbox.Open(nil, sealed[24:], &nonce, &k.key)
	if !ok {
		return nil, errors.New("sealed value does not open with this key")
	}
	return value, nil
}
