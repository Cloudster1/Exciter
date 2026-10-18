package quality

import (
	"slices"
	"strings"
	"testing"

	"github.com/cloudster1/exciter/backend/internal/document"
)

func TestRulesFollowRuleIDs(t *testing.T) {
	if len(Rules) != len(RuleIDs) {
		t.Fatalf("%d rules in the catalog, %d ids", len(Rules), len(RuleIDs))
	}
	for i, r := range Rules {
		if r.ID != RuleIDs[i] {
			t.Errorf("rule %d is %s, want %s", i, r.ID, RuleIDs[i])
		}
		if r.Words != slices.Contains(WordRules, r.ID) {
			t.Errorf("%s takes words: %v", r.ID, r.Words)
		}
		if r.Label == "" || r.Description == "" || len(r.Fields) == 0 || Weight(r.DefaultSeverity) == 0 {
			t.Errorf("%s is not fully described", r.ID)
		}
		if got, ok := RuleOf(r.ID); !ok || got.ID != r.ID {
			t.Errorf("RuleOf(%s) = %v %v", r.ID, got, ok)
		}
		if message, fix := Explain(r.ID, "x"); message == "" || fix == "" {
			t.Errorf("%s is not explained", r.ID)
		}
	}
	if _, ok := RuleOf("spelling"); ok {
		t.Error("an unknown rule is in the catalog")
	}
}

func TestEveryLanguageHasEveryWordList(t *testing.T) {
	for _, lang := range Languages {
		for _, rule := range WordRules {
			if len(builtIn[lang][rule]) == 0 {
				t.Errorf("%s has no %s words", lang, rule)
			}
		}
	}
}

func TestScoreCapsEachRule(t *testing.T) {
	var findings []Judged
	for range 3 {
		findings = append(findings, Judged{Rule: RuleVague, Severity: SeverityWarning, Active: true})
	}
	findings = append(findings,
		Judged{Rule: RuleEscape, Severity: SeverityError, Active: true},
		Judged{Rule: RuleEscape, Severity: SeverityError, Active: false},
		Judged{Rule: RuleEARS, Severity: SeverityHint, Active: true})
	if got, want := Score(findings), FullScore-2*WeightWarning-WeightError-WeightHint; got != want {
		t.Errorf("score = %d, want %d", got, want)
	}
	if Score(nil) != FullScore {
		t.Error("a requirement without findings scores less than full")
	}
	var many []Judged
	for _, rule := range RuleIDs {
		for range 3 {
			many = append(many, Judged{Rule: rule, Severity: SeverityError, Active: true})
		}
	}
	if Score(many) != 0 {
		t.Errorf("the score falls below 0: %d", Score(many))
	}
}

func keysIn(hits []Hit, block int) []string {
	var out []string
	for _, h := range hits {
		if h.Field == FieldStatement && h.Block == block {
			out = append(out, h.Key)
		}
	}
	return out
}

func paragraphs(texts ...string) document.Node {
	root := document.Node{Type: "doc"}
	for _, text := range texts {
		root.Content = append(root.Content, document.Node{Type: "paragraph", Content: []document.Node{{Type: "text", Text: text}}})
	}
	return root
}

func TestKeysStayWhenAnotherSentenceChanges(t *testing.T) {
	second := "The data shall be encrypted and all logs shall be kept."
	before := Examine(Input{Statement: paragraphs("The pump shall start.", second)})
	after := Examine(Input{Statement: paragraphs("The pump shall start within 5 seconds of a request.", second)})
	if got := keysIn(before.Hits, 1); len(got) < 3 || !slices.Equal(got, keysIn(after.Hits, 1)) {
		t.Errorf("keys changed with another sentence: %v, then %v", got, keysIn(after.Hits, 1))
	}
	changed := Examine(Input{Statement: paragraphs("The pump shall start.", strings.Replace(second, "logs", "records", 1))})
	if slices.Equal(keysIn(before.Hits, 1), keysIn(changed.Hits, 1)) {
		t.Error("a finding of shape keeps its key when its own sentence changes")
	}
	for _, h := range before.Hits {
		if len(h.Key) > 200 {
			t.Errorf("key %q is too long", h.Key)
		}
	}
	var twice []string
	for _, h := range Examine(Input{Statement: paragraphs("All data and all logs.")}).Hits {
		if h.Rule == RuleUnbounded {
			twice = append(twice, h.Key)
		}
	}
	if !slices.Equal(twice, []string{"unbounded:statement:all:0", "unbounded:statement:all:1"}) {
		t.Errorf("the same words twice are not counted: %v", twice)
	}
}

func TestTitleAndStatementSpans(t *testing.T) {
	statement := document.Node{Type: "doc", Content: []document.Node{
		{Type: "paragraph", Content: []document.Node{
			{Type: "mention", Attrs: map[string]any{"id": "0195f000-0000-7000-8000-00000000000a", "label": "Ada"}},
			{Type: "text", Text: " shall respond fast."}}},
		{Type: "paragraph", Content: []document.Node{{Type: "text", Text: "\U0001F600 The reply shall be quick."}}},
	}}
	got := Examine(Input{Title: "\U0001F600 Fast export", Statement: statement}).Hits
	want := []Span{
		{Field: FieldTitle, Block: 0, From: 3, To: 7},
		{Field: FieldStatement, Block: 0, From: 16, To: 20},
		{Field: FieldStatement, Block: 1, From: 22, To: 27},
	}
	var spans []Span
	for _, h := range got {
		if h.Rule == RuleVague {
			spans = append(spans, h.Span)
		}
	}
	if !slices.Equal(spans, want) {
		t.Errorf("vague spans = %v, want %v", spans, want)
	}
	if got[0].Field != FieldTitle {
		t.Error("the title's findings come first")
	}
}

func TestAMentionIsNotChecked(t *testing.T) {
	statement := document.Node{Type: "doc", Content: []document.Node{{Type: "paragraph", Content: []document.Node{
		{Type: "text", Text: "The system shall notify "},
		{Type: "mention", Attrs: map[string]any{"id": "0195f000-0000-7000-8000-00000000000a", "label": "Fast Eddie"}},
		{Type: "text", Text: "."}}}}}
	for _, h := range Examine(Input{Statement: statement}).Hits {
		t.Errorf("a person's name is found: %s %q", h.Rule, h.Match)
	}
}

func TestProjectWordsAndAvoidedTerms(t *testing.T) {
	in := Input{
		Statement: paragraphs("The client shall get a snappy and fast reply from the end  user."),
		Words:     map[RuleID]Words{RuleVague: {Add: []string{"snappy"}, Except: []string{"FAST"}}},
		Avoid:     []AvoidTerm{{Word: "client", Use: "customer"}, {Word: "end user", Use: "operator"}},
	}
	var vague, avoided []string
	for _, h := range Examine(in).Hits {
		switch h.Rule {
		case RuleVague:
			vague = append(vague, h.Match)
		case RuleAvoidTerm:
			avoided = append(avoided, h.Match)
		}
	}
	if !slices.Equal(vague, []string{"snappy"}) {
		t.Errorf("vague = %q", vague)
	}
	if !slices.Equal(avoided, []string{"client", "end  user"}) {
		t.Errorf("avoided = %q", avoided)
	}
}

func TestSentencesHaveShapes(t *testing.T) {
	result := Examine(Input{Statement: paragraphs(
		"When the door opens, the light shall turn on. The light is white.",
		"While the pump runs, if the door opens, then the pump shall stop.",
		"Once the door opens, the light shall turn on.")})
	var got []Pattern
	for _, s := range result.Sentences {
		got = append(got, s.Pattern)
	}
	if want := []Pattern{PatternEvent, PatternComplex, PatternNone}; !slices.Equal(got, want) {
		t.Errorf("patterns = %v, want %v", got, want)
	}
	if s := result.Sentences[0]; s.From != 0 || s.To != len("When the door opens, the light shall turn on.") {
		t.Errorf("the first sentence spans %d-%d", s.From, s.To)
	}
}

func TestCodeIsNotProse(t *testing.T) {
	statement := document.Node{Type: "doc", Content: []document.Node{
		{Type: "codeBlock", Content: []document.Node{{Type: "text", Text: "// TODO: all fast"}}},
		{Type: "paragraph", Content: []document.Node{{Type: "text", Text: "The system shall log errors."}}},
	}}
	for _, h := range Examine(Input{Statement: statement}).Hits {
		t.Errorf("code is checked: %s %q", h.Rule, h.Match)
	}
}

func TestExamineReadsNothingElseTheSameEveryTime(t *testing.T) {
	in := Input{Language: German, Title: "Schnelle Anmeldung", Statement: paragraphs(
		"Das System muss alle Daten schnell verschlüsseln und muss sie ggf. löschen.", "Es speichert die Daten.")}
	first := Examine(in)
	for range 5 {
		if again := Examine(in); !slices.Equal(again.Hits, first.Hits) {
			t.Fatal("two checks of the same text differ")
		}
	}
	if len(Examine(Input{}).Hits) != 0 {
		t.Error("an empty requirement finds something")
	}
	if !slices.Equal(Examine(Input{Language: "fr", Statement: in.Statement}).Hits, Examine(Input{Language: English, Statement: in.Statement}).Hits) {
		t.Error("an unknown language does not read as English")
	}
}

func TestConfigHashChangesWithWords(t *testing.T) {
	base := ConfigHash(English, nil, nil)
	if len(base) != 64 {
		t.Fatalf("hash %q is not hex sha256", base)
	}
	same := []string{
		ConfigHash(English, map[RuleID]Words{}, nil),
		ConfigHash(English, map[RuleID]Words{RuleVague: {}}, []AvoidTerm{}),
		ConfigHash("fr", nil, nil),
	}
	for _, h := range same {
		if h != base {
			t.Error("an empty setting changes the hash")
		}
	}
	words := map[RuleID]Words{RuleVague: {Add: []string{"snappy", "zippy"}, Except: []string{"Fast"}}}
	reordered := map[RuleID]Words{RuleVague: {Add: []string{"zippy", " snappy "}, Except: []string{"fast"}}}
	if ConfigHash(English, words, nil) != ConfigHash(English, reordered, nil) {
		t.Error("the order of a list changes the hash")
	}
	differ := []string{
		ConfigHash(German, nil, nil),
		ConfigHash(English, words, nil),
		ConfigHash(English, map[RuleID]Words{RuleEscape: words[RuleVague]}, nil),
		ConfigHash(English, map[RuleID]Words{RuleVague: {Except: []string{"fast"}}}, nil),
		ConfigHash(English, nil, []AvoidTerm{{Word: "client", Use: "customer"}}),
	}
	seen := map[string]bool{base: true}
	for i, h := range differ {
		if seen[h] {
			t.Errorf("setting %d does not change the hash", i)
		}
		seen[h] = true
	}
}

func TestExplainSaysWhatToDo(t *testing.T) {
	if m, _ := Explain(RuleVague, "fast"); m != `"fast" means what each reader wants it to mean.` {
		t.Errorf("message = %q", m)
	}
	if m, _ := Explain(RuleEARS, "If"); !strings.Contains(m, "then") {
		t.Errorf("an If without then is not said so: %q", m)
	}
}
