// Package secrets seals private tokens using the legacy AES-256-GCM wire format.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
)

var ErrCiphertext = errors.New("invalid encrypted token or encryption key")

type Cipher struct{ aead cipher.AEAD }

func New(key [32]byte) (*Cipher, error) {
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Cipher{aead: aead}, nil
}

// Seal preserves the original base64(nonce || ciphertext || tag) representation.
func (c *Cipher) Seal(plaintext string) (string, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(c.aead.Seal(nonce, nonce, []byte(plaintext), nil)), nil
}

func (c *Cipher) Open(encoded string) (string, error) {
	data, err := base64.StdEncoding.DecodeString(encoded)
	n := c.aead.NonceSize()
	if err != nil || len(data) < n+c.aead.Overhead() {
		return "", ErrCiphertext
	}
	plain, err := c.aead.Open(nil, data[:n], data[n:], nil)
	if err != nil {
		return "", ErrCiphertext
	}
	return string(plain), nil
}
