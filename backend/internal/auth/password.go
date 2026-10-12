package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// MinPasswordLength is the fewest characters a password may have. Length is
// the one rule worth having: composition rules make passwords easier to guess.
const MinPasswordLength = 12

// ErrPasswordTooShort is returned by every door that takes a new password.
var ErrPasswordTooShort = fmt.Errorf("password must be at least %d characters", MinPasswordLength)

// ValidatePassword is the one rule, so every door that sets a password agrees.
func ValidatePassword(password string) error {
	if len([]rune(password)) < MinPasswordLength {
		return ErrPasswordTooShort
	}
	return nil
}

// PasswordParams are the argon2id costs. Every hash carries its own, so raising
// them later leaves old hashes valid until the next login rehashes them.
type PasswordParams struct {
	MemoryKiB   uint32
	Iterations  uint32
	Parallelism uint8
	SaltLength  uint32
	KeyLength   uint32
}

// DefaultPasswordParams follows the OWASP argon2id recommendation of 64 MiB
// with three iterations.
func DefaultPasswordParams() PasswordParams {
	return PasswordParams{
		MemoryKiB:   64 * 1024,
		Iterations:  3,
		Parallelism: 4,
		SaltLength:  16,
		KeyLength:   32,
	}
}

// HashConcurrency is how many passwords are hashed at once. Argon2 is meant to
// be expensive, so unbounded callers would be a way to exhaust the machine.
const HashConcurrency = 4

var hashers = make(chan struct{}, HashConcurrency)

func hashing() func() {
	hashers <- struct{}{}
	return func() { <-hashers }
}

// HashPassword returns an argon2id hash in the PHC string format, which carries
// its own parameters: $argon2id$v=19$m=65536,t=3,p=4$<salt>$<hash>.
func HashPassword(password string, p PasswordParams) (string, error) {
	if password == "" {
		return "", errors.New("password must not be empty")
	}
	salt := make([]byte, p.SaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}
	defer hashing()()
	key := argon2.IDKey([]byte(password), salt, p.Iterations, p.MemoryKiB, p.Parallelism, p.KeyLength)

	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, p.MemoryKiB, p.Iterations, p.Parallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

// VerifyPassword reports whether password matches encoded, and whether the hash
// should be upgraded because it was made with weaker parameters than current.
func VerifyPassword(password, encoded string, current PasswordParams) (ok bool, needsRehash bool, err error) {
	p, salt, want, err := decodeHash(encoded)
	if err != nil {
		return false, false, err
	}
	release := hashing()
	got := argon2.IDKey([]byte(password), salt, p.Iterations, p.MemoryKiB, p.Parallelism, uint32(len(want)))
	release()
	if subtle.ConstantTimeCompare(got, want) != 1 {
		return false, false, nil
	}
	weaker := p.MemoryKiB < current.MemoryKiB ||
		p.Iterations < current.Iterations ||
		p.Parallelism < current.Parallelism ||
		uint32(len(want)) < current.KeyLength
	return true, weaker, nil
}

func decodeHash(encoded string) (PasswordParams, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	// "", "argon2id", "v=19", "m=...,t=...,p=...", salt, hash
	if len(parts) != 6 || parts[0] != "" {
		return PasswordParams{}, nil, nil, errors.New("malformed password hash")
	}
	if parts[1] != "argon2id" {
		return PasswordParams{}, nil, nil, fmt.Errorf("unsupported password hash algorithm %q", parts[1])
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return PasswordParams{}, nil, nil, errors.New("malformed password hash version")
	}
	if version != argon2.Version {
		return PasswordParams{}, nil, nil, fmt.Errorf("unsupported argon2 version %d", version)
	}

	var p PasswordParams
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.MemoryKiB, &p.Iterations, &p.Parallelism); err != nil {
		return PasswordParams{}, nil, nil, errors.New("malformed password hash parameters")
	}

	salt, err := base64.RawStdEncoding.Strict().DecodeString(parts[4])
	if err != nil {
		return PasswordParams{}, nil, nil, errors.New("malformed password hash salt")
	}
	key, err := base64.RawStdEncoding.Strict().DecodeString(parts[5])
	if err != nil {
		return PasswordParams{}, nil, nil, errors.New("malformed password hash digest")
	}
	p.SaltLength = uint32(len(salt))
	p.KeyLength = uint32(len(key))
	return p, salt, key, nil
}
