package baseline

import "testing"

// The sentence a signer confirms names the baseline and its project in their language, English unless it is German.
func TestTheMeaningOfASignatureIsInTheSignersLanguage(t *testing.T) {
	for _, c := range []struct{ lang, want string }{
		{"en", "I release the baseline Release 1.0 of CHK as the agreed state of its requirements."},
		{"de", "Ich gebe die Baseline Release 1.0 von CHK als vereinbarten Stand ihrer Anforderungen frei."},
		{"", "I release the baseline Release 1.0 of CHK as the agreed state of its requirements."},
		{"fr", "I release the baseline Release 1.0 of CHK as the agreed state of its requirements."},
	} {
		if got := meaning(c.lang, "Release 1.0", "CHK"); got != c.want {
			t.Errorf("in %q: %q, want %q", c.lang, got, c.want)
		}
	}
}
