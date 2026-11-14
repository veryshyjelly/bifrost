package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"errors"
	"testing"
)

func TestCipherPreservesBaselineLayoutAndAuthenticates(t *testing.T) {
	var key [32]byte
	key[0] = 42
	c, err := New(key)
	if err != nil {
		t.Fatal(err)
	}
	first, err := c.Seal("private token")
	if err != nil {
		t.Fatal(err)
	}
	second, err := c.Seal("private token")
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("nonce reused")
	}
	if plain, err := c.Open(first); err != nil || plain != "private token" {
		t.Fatal("round trip failed")
	}
	// Build the original nonce/ciphertext/tag layout independently with a fixed fixture nonce.
	block, _ := aes.NewCipher(key[:])
	aead, _ := cipher.NewGCM(block)
	nonce := make([]byte, 12)
	baseline := base64.StdEncoding.EncodeToString(aead.Seal(nonce, nonce, []byte("baseline token"), nil))
	if plain, err := c.Open(baseline); err != nil || plain != "baseline token" {
		t.Fatal("baseline token unreadable")
	}
	bytes, _ := base64.StdEncoding.DecodeString(first)
	bytes[len(bytes)-1] ^= 1
	for _, bad := range []string{"", "not base64", base64.StdEncoding.EncodeToString(bytes)} {
		if _, err := c.Open(bad); !errors.Is(err, ErrCiphertext) {
			t.Fatal("malformed/tampered token accepted")
		}
	}
	other, _ := New([32]byte{})
	if _, err := other.Open(first); !errors.Is(err, ErrCiphertext) {
		t.Fatal("wrong key accepted")
	}
}
