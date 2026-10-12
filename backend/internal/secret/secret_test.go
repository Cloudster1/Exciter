package secret

import (
	"bytes"
	"errors"
	"testing"
)

func key(fill byte) []byte { return bytes.Repeat([]byte{fill}, KeySize) }

func TestSealedValuesOpenOnlyWithTheirKeyAndContext(t *testing.T) {
	box, err := New(key(1))
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := box.Seal([]byte("client secret"), []byte("org-a"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, []byte("client secret")) {
		t.Fatal("the plaintext is visible in the sealed value")
	}
	plain, err := box.Open(sealed, []byte("org-a"))
	if err != nil || string(plain) != "client secret" {
		t.Fatalf("open = %q, %v", plain, err)
	}

	if _, err := box.Open(sealed, []byte("org-b")); !errors.Is(err, ErrTampered) {
		t.Errorf("a secret moved to another record opened: %v", err)
	}
	other, _ := New(key(2))
	if _, err := other.Open(sealed, []byte("org-a")); !errors.Is(err, ErrTampered) {
		t.Errorf("another key opened the secret: %v", err)
	}
	flipped := append([]byte(nil), sealed...)
	flipped[len(flipped)-1] ^= 1
	if _, err := box.Open(flipped, []byte("org-a")); !errors.Is(err, ErrTampered) {
		t.Errorf("an altered secret opened: %v", err)
	}
	if _, err := box.Open([]byte{version}, nil); !errors.Is(err, ErrTampered) {
		t.Errorf("a truncated value opened: %v", err)
	}
}

func TestSealingTwiceGivesDifferentValues(t *testing.T) {
	box, _ := New(key(3))
	a, _ := box.Seal([]byte("same"), nil)
	b, _ := box.Seal([]byte("same"), nil)
	if bytes.Equal(a, b) {
		t.Fatal("two seals of one value are identical, so the nonce is not fresh")
	}
}

func TestWithoutAKeyNothingIsSealed(t *testing.T) {
	var box *Box
	if _, err := box.Seal([]byte("x"), nil); !errors.Is(err, ErrNoKey) {
		t.Errorf("seal without a key = %v", err)
	}
	if _, err := New([]byte("short")); err == nil {
		t.Error("a short key was accepted")
	}
}
