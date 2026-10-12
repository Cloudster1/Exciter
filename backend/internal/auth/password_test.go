package auth

import (
	"errors"
	"strings"
	"testing"
)

// testParams keeps argon2 cheap enough for unit tests while staying a real
// argon2id hash; the production cost is exercised by the decoy test.
func testParams() PasswordParams {
	p := DefaultPasswordParams()
	p.MemoryKiB = 8 * 1024
	p.Iterations = 1
	return p
}

func TestHashAndVerify(t *testing.T) {
	p := testParams()
	hash, err := HashPassword("correct horse battery staple", p)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}

	ok, rehash, err := VerifyPassword("correct horse battery staple", hash, p)
	if err != nil {
		t.Fatalf("VerifyPassword: %v", err)
	}
	if !ok {
		t.Error("correct password did not verify")
	}
	if rehash {
		t.Error("hash written with current parameters should not need a rehash")
	}

	ok, _, err = VerifyPassword("wrong password", hash, p)
	if err != nil {
		t.Fatalf("VerifyPassword: %v", err)
	}
	if ok {
		t.Error("wrong password verified")
	}
}

func TestHashesAreSalted(t *testing.T) {
	p := testParams()
	a, err := HashPassword("same password", p)
	if err != nil {
		t.Fatal(err)
	}
	b, err := HashPassword("same password", p)
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Error("two hashes of the same password are identical, so the salt is not random")
	}
}

func TestVerifyDetectsWeakerParameters(t *testing.T) {
	weak := testParams()
	hash, err := HashPassword("hunter2", weak)
	if err != nil {
		t.Fatal(err)
	}

	stronger := weak
	stronger.MemoryKiB *= 2
	ok, rehash, err := VerifyPassword("hunter2", hash, stronger)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("password should still verify against its own parameters")
	}
	if !rehash {
		t.Error("a hash weaker than the current policy should be flagged for rehashing")
	}
}

func TestVerifyRejectsMalformedHashes(t *testing.T) {
	p := testParams()
	for _, bad := range []string{
		"",
		"not-a-hash",
		"$argon2i$v=19$m=8192,t=1,p=4$c2FsdA$aGFzaA",
		"$argon2id$v=16$m=8192,t=1,p=4$c2FsdA$aGFzaA",
		"$argon2id$v=19$m=bad,t=1,p=4$c2FsdA$aGFzaA",
		"$argon2id$v=19$m=8192,t=1,p=4$!!!notbase64$aGFzaA",
	} {
		if _, _, err := VerifyPassword("x", bad, p); err == nil {
			t.Errorf("VerifyPassword(%q) returned no error", bad)
		}
	}
}

func TestEmptyPasswordRejected(t *testing.T) {
	if _, err := HashPassword("", testParams()); err == nil {
		t.Error("empty password should not be hashable")
	}
}

func TestPasswordsNeedTwelveCharacters(t *testing.T) {
	if err := ValidatePassword("elevenchars"); !errors.Is(err, ErrPasswordTooShort) {
		t.Errorf("eleven characters = %v, want ErrPasswordTooShort", err)
	}
	// Characters, not bytes: twelve umlauts are twelve characters.
	if err := ValidatePassword(strings.Repeat("\u00e4", MinPasswordLength)); err != nil {
		t.Errorf("twelve characters = %v", err)
	}
}

// Were the decoy malformed, verifying would return early, and the timing would
// tell which addresses have accounts.
func TestDecoyHashPerformsRealWork(t *testing.T) {
	ok, _, err := VerifyPassword("anything at all", decoyHash, DefaultPasswordParams())
	if err != nil {
		t.Fatalf("decoy hash is malformed, so it short-circuits instead of costing work: %v", err)
	}
	if ok {
		t.Fatal("decoy hash must never verify against a user-supplied password")
	}
}
