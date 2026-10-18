package quality

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/document"
)

// stemsOf stems each word of a phrase as the glossary does.
func stemsOf(lang Language, phrase string) []string {
	var out []string
	for _, t := range tokenize(phrase, 0, len(phrase)) {
		out = append(out, Stem(lang, t.text))
	}
	return out
}

func TestStemsFollowTheTables(t *testing.T) {
	for _, lang := range Languages {
		path := filepath.Join("testdata", "terms", string(lang)+".txt")
		f, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		scan := bufio.NewScanner(f)
		lines := 0
		for n := 1; scan.Scan(); n++ {
			line := strings.TrimSpace(scan.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			lines++
			where := fmt.Sprintf("%s:%d", path, n)
			if word, stem, ok := strings.Cut(line, " -> "); ok {
				if got := Stem(lang, word); got != stem {
					t.Errorf("%s: %q stems to %q, want %q", where, word, got, stem)
				}
				continue
			}
			if a, b, ok := strings.Cut(line, " != "); ok {
				if slices.Equal(stemsOf(lang, a), stemsOf(lang, b)) {
					t.Errorf("%s: %q and %q meet as %q", where, a, b, stemsOf(lang, a))
				}
				continue
			}
			a, b, ok := strings.Cut(line, " = ")
			if !ok {
				t.Fatalf("%s: a line is word -> stem, a = b or a != b", where)
			}
			if sa, sb := stemsOf(lang, a), stemsOf(lang, b); !slices.Equal(sa, sb) {
				t.Errorf("%s: %q stems to %q and %q to %q", where, a, sa, b, sb)
			}
		}
		f.Close()
		if err := scan.Err(); err != nil || lines == 0 {
			t.Fatalf("%s: %v, %d lines", path, err, lines)
		}
	}
}

var (
	pedalID    = uuid.MustParse("0195f000-0000-7000-8000-0000000000a1")
	brakeID    = uuid.MustParse("0195f000-0000-7000-8000-0000000000a2")
	customerID = uuid.MustParse("0195f000-0000-7000-8000-0000000000a3")
	absID      = uuid.MustParse("0195f000-0000-7000-8000-0000000000a4")
	leverID    = uuid.MustParse("0195f000-0000-7000-8000-0000000000a5")
	footID     = uuid.MustParse("0195f000-0000-7000-8000-0000000000a6")
	clientID   = uuid.MustParse("0195f000-0000-7000-8000-0000000000a7")
)

func term(id uuid.UUID, name string, rank int, synonyms []string, avoid ...string) Term {
	return Term{ID: id, Name: name, Synonyms: synonyms, Avoid: avoid, Rank: rank}
}

// described lists uses as "field block from-to name words", with "linked" after an explicit one.
func described(uses []TermUse, terms []Term) []string {
	var out []string
	for _, u := range uses {
		name := ""
		if i := slices.IndexFunc(terms, func(t Term) bool { return t.ID == u.TermID }); i >= 0 {
			name = terms[i].Name
		}
		s := fmt.Sprintf("%s %d %d-%d %s: %s", u.Field, u.Block, u.From, u.To, name, u.Words)
		if u.Explicit {
			s += " linked"
		}
		out = append(out, s)
	}
	return out
}

func avoidedIn(hits []Hit) []string {
	var out []string
	for _, h := range hits {
		if h.Rule == RuleAvoidTerm {
			out = append(out, fmt.Sprintf("%s %d %d-%d %s > %s", h.Field, h.Block, h.From, h.To, h.Match, h.Use))
		}
	}
	return out
}

func TestTermsAreFoundByTheirWords(t *testing.T) {
	pedal := term(pedalID, "brake pedal", 0, []string{"foot brake"})
	brake := term(brakeID, "brake", 0, nil)
	abs := term(absID, "ABS", 0, []string{"ECU"})
	for _, c := range []struct {
		name  string
		lang  Language
		title string
		text  []string
		terms []Term
		want  []string
	}{
		{name: "a synonym", text: []string{"The driver shall press the foot brake."}, terms: []Term{pedal},
			want: []string{"statement 0 27-37 brake pedal: foot brake"}},
		{name: "a plural", text: []string{"The brake pedals shall return."}, terms: []Term{pedal},
			want: []string{"statement 0 4-16 brake pedal: brake pedals"}},
		{name: "a possessive and a capital", text: []string{"The Brake Pedal's travel shall be 40 mm."}, terms: []Term{pedal},
			want: []string{"statement 0 4-17 brake pedal: Brake Pedal's"}},
		{name: "German cases", lang: German, text: []string{"Die Bremsen der Züge müssen greifen."},
			terms: []Term{term(brakeID, "Bremse", 0, []string{"Zug"})},
			want:  []string{"statement 0 4-11 Bremse: Bremsen", "statement 0 16-20 Bremse: Züge"}},
		{name: "an acronym as written", text: []string{"The ABS shall act; abs are muscles, and the ECUs report."}, terms: []Term{abs},
			want: []string{"statement 0 4-7 ABS: ABS", "statement 0 44-48 ABS: ECUs"}},
		{name: "the longest match", text: []string{"The brake pedal moves the brake."}, terms: []Term{brake, pedal},
			want: []string{"statement 0 4-15 brake pedal: brake pedal", "statement 0 26-31 brake: brake"}},
		{name: "words apart are not a phrase", text: []string{"Brake, pedal and brake\npedal."}, terms: []Term{pedal},
			want: nil},
		{name: "the first glossary wins a shared word", text: []string{"Press the lever."},
			terms: []Term{term(leverID, "lever", 1, nil), term(footID, "foot lever", 0, []string{"lever"})},
			want:  []string{"statement 0 10-15 foot lever: lever"}},
		{name: "then the name", text: []string{"Press the lever."},
			terms: []Term{term(leverID, "lever", 0, nil), term(footID, "foot lever", 0, []string{"lever"})},
			want:  []string{"statement 0 10-15 foot lever: lever"}},
		{name: "the title first", title: "Brake pedal travel", text: []string{"The foot brake shall travel 40 mm."}, terms: []Term{pedal},
			want: []string{"title 0 0-11 brake pedal: Brake pedal", "statement 0 4-14 brake pedal: foot brake"}},
		{name: "a term used twice", text: []string{"The brake shall hold.", "A brake shall hold, and the brake shall stay."}, terms: []Term{brake},
			want: []string{"statement 0 4-9 brake: brake", "statement 1 2-7 brake: brake", "statement 1 28-33 brake: brake"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := Examine(Input{Language: c.lang, Title: c.title, Statement: paragraphs(c.text...), Terms: c.terms})
			if got := described(r.Uses, c.terms); !slices.Equal(got, c.want) {
				t.Errorf("uses = %q\nwant   %q", got, c.want)
			}
		})
	}
}

func TestPositionsCountAsTheEditorDoes(t *testing.T) {
	statement := document.Node{Type: "doc", Content: []document.Node{
		{Type: "codeBlock", Content: []document.Node{{Type: "text", Text: "brake_pedal := brake pedal"}}},
		{Type: "paragraph", Content: []document.Node{
			{Type: "mention", Attrs: map[string]any{"id": "0195f000-0000-7000-8000-00000000000a", "label": "Brake Pedal"}},
			{Type: "text", Text: " shall press the \U0001F642 brake pedal."}}},
	}}
	terms := []Term{term(pedalID, "brake pedal", 0, nil)}
	r := Examine(Input{Title: "\U0001F600 Brake pedal", Statement: statement, Terms: terms})
	want := []string{"title 0 3-14 brake pedal: Brake pedal", "statement 1 21-32 brake pedal: brake pedal"}
	if got := described(r.Uses, terms); !slices.Equal(got, want) {
		t.Errorf("uses = %q, want %q", got, want)
	}
}

func linked(id uuid.UUID, text string, marks ...document.Mark) document.Node {
	marks = append(marks, document.Mark{Type: document.MarkTerm, Attrs: map[string]any{"termId": id.String()}})
	return document.Node{Type: "text", Text: text, Marks: marks}
}

func TestExplicitLinksAreUses(t *testing.T) {
	pedal := term(pedalID, "brake pedal", 0, nil)
	brake := term(brakeID, "brake", 0, nil)
	unknown := uuid.MustParse("0195f000-0000-7000-8000-0000000000ff")
	statement := document.Node{Type: "doc", Content: []document.Node{{Type: "paragraph", Content: []document.Node{
		{Type: "text", Text: "Press "},
		linked(pedalID, "the stopping "),
		linked(pedalID, "lever", document.Mark{Type: "bold"}),
		{Type: "text", Text: ", then "},
		linked(pedalID, "the brake pedal"),
		{Type: "text", Text: " and "},
		linked(unknown, "the brake"),
		{Type: "text", Text: "."},
	}}}}
	terms := []Term{pedal, brake}
	r := Examine(Input{Statement: statement, Terms: terms})
	want := []string{
		"statement 0 6-24 brake pedal: the stopping lever linked",
		"statement 0 31-46 brake pedal: the brake pedal linked",
		"statement 0 55-60 brake: brake",
	}
	if got := described(r.Uses, terms); !slices.Equal(got, want) {
		t.Errorf("uses = %q\nwant   %q", got, want)
	}
	// A link to another term keeps the words' own term beside it.
	other := document.Node{Type: "doc", Content: []document.Node{{Type: "paragraph", Content: []document.Node{
		linked(pedalID, "brake"), {Type: "text", Text: " now."}}}}}
	want = []string{"statement 0 0-5 brake pedal: brake linked", "statement 0 0-5 brake: brake"}
	if got := described(Examine(Input{Statement: other, Terms: terms}).Uses, terms); !slices.Equal(got, want) {
		t.Errorf("uses = %q, want %q", got, want)
	}
}

func TestWordsToAvoidNameTheTermToUse(t *testing.T) {
	customer := term(customerID, "customer", 0, nil, "client", "end user")
	terms := []Term{customer}
	r := Examine(Input{Title: "Clients pay", Statement: paragraphs("The end users and the client shall pay the customer."), Terms: terms})
	want := []string{"title 0 0-7 Clients > customer", "statement 0 4-13 end users > customer", "statement 0 22-28 client > customer"}
	if got := avoidedIn(r.Hits); !slices.Equal(got, want) {
		t.Errorf("avoided = %q\nwant      %q", got, want)
	}
	if got := described(r.Uses, terms); !slices.Equal(got, []string{"statement 0 43-51 customer: customer"}) {
		t.Errorf("uses = %q", got)
	}
	for _, h := range r.Hits {
		if h.Rule == RuleAvoidTerm && !strings.HasPrefix(h.Key, "avoid_term:") {
			t.Errorf("key %q", h.Key)
		}
	}
	// The project's own term is a word it uses, whatever a library says of it.
	own := term(clientID, "client", 0, nil)
	library := term(customerID, "customer", 1, nil, "client")
	terms = []Term{library, own}
	r = Examine(Input{Statement: paragraphs("The client shall pay."), Terms: terms})
	if got := avoidedIn(r.Hits); len(got) != 0 {
		t.Errorf("a word of the project's own glossary is to be avoided: %q", got)
	}
	if got := described(r.Uses, terms); !slices.Equal(got, []string{"statement 0 4-10 client: client"}) {
		t.Errorf("uses = %q", got)
	}
}

func TestNoTermsFindNothing(t *testing.T) {
	r := Examine(Input{Statement: paragraphs("The brake pedal shall return.")})
	if len(r.Uses) != 0 || len(avoidedIn(r.Hits)) != 0 {
		t.Errorf("found %v without a glossary", r.Uses)
	}
}
