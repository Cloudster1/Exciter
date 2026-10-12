package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
)

// Session secrets are never stored, only their SHA-256, so a copy of the database opens nothing. A plain hash
// rather than argon2, because the secret is 256 random bits: there is nothing to guess.

// tokenBytes is the raw entropy behind a token, before encoding.
const tokenBytes = 32

// GenerateToken returns a new opaque secret and the digest to store for it.
func GenerateToken() (secret string, digest []byte, err error) {
	raw := make([]byte, tokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, fmt.Errorf("generate token: %w", err)
	}
	secret = base64.RawURLEncoding.EncodeToString(raw)
	return secret, HashToken(secret), nil
}

// HashToken returns the digest stored for a token secret.
func HashToken(secret string) []byte {
	sum := sha256.Sum256([]byte(secret))
	return sum[:]
}
