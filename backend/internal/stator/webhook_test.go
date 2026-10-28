package stator

import (
	"testing"

	"github.com/google/uuid"
)

func TestASignatureIsStators(t *testing.T) {
	// RFC 4231's second HMAC-SHA256 case, in the shape Stator's webhook.Sign sends.
	if got := Sign([]byte("what do ya want for nothing?"), "Jefe"); got != "sha256=5bdcc146bf60754e6a042426089575c75a003f089d2739839dec58b964ec3843" {
		t.Errorf("signature %q", got)
	}
	body, secret := []byte(`{"id":"x"}`), WebhookSecretPrefix+"secret"
	if Sign(body, secret) == Sign([]byte(`{"id":"y"}`), secret) {
		t.Error("a signature of another body matches")
	}
	if Sign(body, secret) == Sign(body, secret+"x") {
		t.Error("a signature under another secret matches")
	}
	if Sign(body, secret) == Sign(body, absentSecret) {
		t.Error("a delivery to an installation without a secret matches one with")
	}
}

func TestOnlyThisInstallationsRequirementLinksAreRead(t *testing.T) {
	s := &Service{opts: Options{AppURL: "https://exciter.example/"}}
	id := uuid.New()
	if got, ok := s.requirementOf("https://exciter.example" + RequirementPath + id.String()); !ok || got != id {
		t.Errorf("a requirement's link reads as %v %v", got, ok)
	}
	for _, other := range []string{
		"https://stator.example" + RequirementPath + id.String(),
		"http://exciter.example" + RequirementPath + id.String(),
		"https://exciter.example" + RequirementPath + "nope",
		"https://exciter.example/p/" + id.String(),
		"https://exciter.example.evil" + RequirementPath + id.String(),
		"",
	} {
		if _, ok := s.requirementOf(other); ok {
			t.Errorf("%q reads as a requirement", other)
		}
	}
}
