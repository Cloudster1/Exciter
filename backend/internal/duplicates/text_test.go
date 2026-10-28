package duplicates

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/cloudster1/exciter/backend/internal/document"
)

func paragraph(text string) document.Node {
	return document.Node{Type: "doc", Content: []document.Node{{Type: "paragraph", Content: []document.Node{{Type: "text", Text: text}}}}}
}

func TestTheTextComparedIsTheTitleAndTheStatementWithinTheLimit(t *testing.T) {
	if got := textOf("Brakes", paragraph("The brakes engage.")); got != "Brakes The brakes engage." {
		t.Errorf("the text reads %q", got)
	}
	if got := textOf("Brakes", document.Node{Type: "doc"}); got != "Brakes " {
		t.Errorf("a title without a statement reads %q", got)
	}
	if got := textOf("", document.Node{Type: "doc"}); strings.TrimSpace(got) != "" {
		t.Errorf("nothing reads %q", got)
	}

	// The limit counts characters, so a word of several bytes is never cut in half.
	long := textOf(strings.Repeat("ö", MaxTextChars), paragraph("tail"))
	if n := utf8.RuneCountInString(long); n != MaxTextChars || !utf8.ValidString(long) || strings.Contains(long, "tail") {
		t.Errorf("a long text keeps %d characters, valid %v", n, utf8.ValidString(long))
	}
	exact := strings.Repeat("a", MaxTextChars-1)
	if got := textOf(exact, document.Node{Type: "doc"}); got != exact+" " {
		t.Errorf("a text at the limit is cut to %d characters", utf8.RuneCountInString(got))
	}
}
