// Package secret encrypts the few secrets Exciter has to keep in the database,
// such as an identity provider's client secret, with AES-256-GCM.
package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
)

// version leads every sealed value, so a later key or algorithm can be told
// apart from this one without guessing.
const version byte = 1

// KeySize is the key length Box needs.
const KeySize = 32

var (
	// ErrNoKey is returned when a secret has to be sealed or opened and no key
	// was configured.
	ErrNoKey = errors.New("no encryption key is configured; set EXCITER_SECRET_KEY")
	// ErrTampered covers a sealed value that does not open with this key: it
	// was altered, belongs to another record, or was sealed under another key.
	ErrTampered = errors.New("a stored secret could not be decrypted with the configured key")
)

// Box seals and opens values. A nil Box has no key and refuses both.
type Box struct {
	aead cipher.AEAD
}

// New returns a Box for a 32 byte key.
func New(key []byte) (*Box, error) {
	if len(key) != KeySize {
		return nil, fmt.Errorf("the encryption key is %d bytes, want %d", len(key), KeySize)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Box{aead: aead}, nil
}

// Seal encrypts plaintext. The context is authenticated but not stored: a
// value sealed for one record cannot be copied onto another and opened there.
func (b *Box) Seal(plaintext, context []byte) ([]byte, error) {
	if b == nil {
		return nil, ErrNoKey
	}
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("read randomness: %w", err)
	}
	out := append([]byte{version}, nonce...)
	return b.aead.Seal(out, nonce, plaintext, context), nil
}

// Open decrypts what Seal produced with the same context.
func (b *Box) Open(sealed, context []byte) ([]byte, error) {
	if b == nil {
		return nil, ErrNoKey
	}
	n := b.aead.NonceSize()
	if len(sealed) < 1+n || sealed[0] != version {
		return nil, ErrTampered
	}
	plain, err := b.aead.Open(nil, sealed[1:1+n], sealed[1+n:], context)
	if err != nil {
		return nil, ErrTampered
	}
	return plain, nil
}
