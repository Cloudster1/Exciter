package armature

import (
	"testing"

	"github.com/google/uuid"
)

func TestASignatureIsArmatures(t *testing.T) {
	// RFC 4231's second HMAC-SHA256 case, in the shape Armature sends.
	if got := Sign([]byte("what do ya want for nothing?"), "Jefe"); got != "sha256=5bdcc146bf60754e6a042426089575c75a003f089d2739839dec58b964ec3843" {
		t.Errorf("signature %q", got)
	}
	if Sign([]byte("a"), "x") == Sign([]byte("b"), "x") || Sign([]byte("a"), "x") == Sign([]byte("a"), "y") {
		t.Error("the signature does not depend on the body and the secret")
	}
	if SignTimestamped([]byte("body"), "x", "1700000000") != Sign([]byte("1700000000.body"), "x") {
		t.Error("the timestamped signature is not over {timestamp}.{body}")
	}
}

func TestHeadersVouchForTheBodyTheyCameWith(t *testing.T) {
	body, secret, at := []byte(`{"id":"x"}`), WebhookSecretPrefix+"secret", "1700000000"
	signed := WebhookHeaders{Signature: Sign(body, secret), Timestamp: at, TimestampedSignature: SignTimestamped(body, secret, at)}
	for name, tc := range map[string]struct {
		headers WebhookHeaders
		body    []byte
		secret  string
		ok      bool
	}{
		"both signatures":               {signed, body, secret, true},
		"an older Armature's one":       {WebhookHeaders{Signature: Sign(body, secret)}, body, secret, true},
		"another secret":                {signed, body, secret + "x", false},
		"another body":                  {signed, []byte(`{"id":"y"}`), secret, false},
		"a timestamp moved":             {WebhookHeaders{Signature: signed.Signature, Timestamp: "1700000001", TimestampedSignature: signed.TimestampedSignature}, body, secret, false},
		"a timestamp without signature": {WebhookHeaders{Signature: signed.Signature, Timestamp: at}, body, secret, false},
		"a timestamp that is no number": {WebhookHeaders{Signature: signed.Signature, Timestamp: "soon", TimestampedSignature: SignTimestamped(body, secret, "soon")}, body, secret, false},
		"no signature":                  {WebhookHeaders{}, body, secret, false},
	} {
		if got := tc.headers.Verify(tc.body, tc.secret); got != tc.ok {
			t.Errorf("%s: verified %v, want %v", name, got, tc.ok)
		}
	}
}

func TestOnlyThisInstallationsRequirementLinksAreRead(t *testing.T) {
	s := &Service{opts: Options{AppURL: "https://exciter.example/"}}
	id := uuid.New()
	if got, ok := s.requirementOf("https://exciter.example/r/" + id.String()); !ok || got != id {
		t.Errorf("a requirement's link reads as %v %v", got, ok)
	}
	for _, other := range []string{"https://stator.example/r/" + id.String(), "https://exciter.example/r/nope", "https://exciter.example/p/" + id.String()} {
		if _, ok := s.requirementOf(other); ok {
			t.Errorf("%s reads as a requirement", other)
		}
	}
}
