package mail

import "testing"

// A subject is somebody's words: a requirement title, a person's name. A newline in
// one of them would start a header of their choosing.
func TestAHeaderStaysOnOneLine(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"Runbook", "Runbook"},
		{"Runbook\r\nBcc: everyone@elsewhere.test", "Runbook  Bcc: everyone@elsewhere.test"},
		{"Line\nbreak", "Line break"},
		{"Bell\aring", "Bell ring"},
	} {
		if got := headerValue(c.in); got != c.want {
			t.Errorf("headerValue(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// A title in another script reaches the inbox as written, not as bytes a
// header may not carry; a plain one is left alone.
func TestASubjectBeyondASCIIIsEncoded(t *testing.T) {
	if got := encodeSubject("Plain words"); got != "Plain words" {
		t.Errorf("a plain subject became %q", got)
	}
	if got := encodeSubject("Überblick"); got != "=?UTF-8?q?=C3=9Cberblick?=" {
		t.Errorf("an umlaut became %q", got)
	}
}
