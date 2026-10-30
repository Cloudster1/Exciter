package notify

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// A person hears once per event, as the first kind of the table that applies to them, and the one who acted never
// hears at all.
func TestPickTellsEachPersonOnceByTheFirstKind(t *testing.T) {
	actor, ann, ben, cat := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	got := Pick(actor, []Tell{
		{UserID: ann, Kind: KindCommented},
		{UserID: actor, Kind: KindMentioned},
		{UserID: ben, Kind: KindRevised},
		{UserID: ann, Kind: KindMentioned},
		{UserID: cat, Kind: KindCommented},
		{UserID: cat, Kind: KindReplied},
		{UserID: ben, Kind: KindRevised},
		{UserID: uuid.Nil, Kind: KindMentioned},
		{UserID: cat, Kind: Kind("unknown")},
	})
	want := []Tell{{UserID: ann, Kind: KindMentioned}, {UserID: ben, Kind: KindRevised}, {UserID: cat, Kind: KindReplied}}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("tell %d = %v, want %v", i, got[i], want[i])
		}
	}
}

// Hourly bundles go at the next full hour after the oldest row, daily ones at the next eight o'clock UTC, and a
// queue left from before off at once.
func TestDueFollowsTheSchedule(t *testing.T) {
	at := func(s string) time.Time {
		v, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	for _, c := range []struct {
		digest      Digest
		oldest, now string
		want        bool
	}{
		{DigestOff, "2026-09-30T10:15:00Z", "2026-09-30T10:15:00Z", true},
		{DigestHourly, "2026-09-30T10:15:00Z", "2026-09-30T10:59:59Z", false},
		{DigestHourly, "2026-09-30T10:15:00Z", "2026-09-30T11:00:00Z", true},
		{DigestDaily, "2026-09-30T07:59:00Z", "2026-09-30T07:59:59Z", false},
		{DigestDaily, "2026-09-30T07:59:00Z", "2026-09-30T08:00:00Z", true},
		{DigestDaily, "2026-09-30T08:00:00Z", "2026-09-30T23:00:00Z", false},
		{DigestDaily, "2026-09-30T08:00:00Z", "2026-10-01T08:00:00Z", true},
		{DigestDaily, "2026-09-30T10:00:00+02:00", "2026-10-01T07:00:00Z", false},
	} {
		if got := Due(c.digest, at(c.oldest), at(c.now)); got != c.want {
			t.Errorf("Due(%s, %s, %s) = %v, want %v", c.digest, c.oldest, c.now, got, c.want)
		}
	}
}

// What nobody saved is everything on; a stored map leaves a kind it does not name on, so a kind added later reaches
// everybody.
func TestPreferencesDefaultToOn(t *testing.T) {
	d := DefaultPreferences()
	for _, k := range Kinds {
		if !On(d.InApp, k) || !On(d.Email, k) || !d.InApp[k] {
			t.Errorf("%s is off by default", k)
		}
	}
	if d.Digest != DigestOff || !d.AutoWatch {
		t.Errorf("the defaults are %+v", d)
	}
	stored := map[Kind]bool{KindRevised: false}
	if On(stored, KindRevised) || !On(stored, KindMentioned) {
		t.Errorf("a map turning revised off reads as revised %v, mentioned %v", On(stored, KindRevised), On(stored, KindMentioned))
	}
	if back := every(stored); len(back) != len(Kinds) || back[KindRevised] || !back[KindTransitioned] {
		t.Errorf("the switches store as %v", back)
	}
}

func TestAnUnknownDigestOrKindIsRefusedOnItsField(t *testing.T) {
	p := DefaultPreferences()
	p.Digest = "weekly"
	fe, ok := p.Validate().(*FieldError)
	if !ok || fe.Field != "digest" || !strings.HasSuffix(fe.Message, ".") {
		t.Fatalf("weekly is refused with %v", fe)
	}
	for _, d := range Digests {
		p.Digest = d
		if err := p.Validate(); err != nil {
			t.Errorf("%s is refused: %v", d, err)
		}
	}
	p.Email = map[Kind]bool{"published": false}
	if fe, ok := p.Validate().(*FieldError); !ok || fe.Field != "email" || !strings.Contains(fe.Message, string(KindMentioned)) {
		t.Fatalf("an unknown kind is refused with %v", fe)
	}
}

// An excerpt is one line of at most MaxExcerptLength characters.
func TestAnExcerptIsOneShortLine(t *testing.T) {
	if got := Excerpt("  Added the\n\nbudget.  "); got != "Added the budget." {
		t.Errorf("got %q", got)
	}
	long := strings.Repeat("ä", MaxExcerptLength+50)
	if got := Excerpt(long); len([]rune(got)) != MaxExcerptLength {
		t.Errorf("a long excerpt keeps %d characters", len([]rune(got)))
	}
}

// Mail is the one place the server words a notification, and it links to the requirement and to the preferences.
func TestAMailSaysWhatHappenedAndWhere(t *testing.T) {
	req, thread := uuid.New(), uuid.New()
	m := mailed{kind: KindRevised, actor: "Alice", requirementID: req, key: "BRK-R4", title: "Stopping distance",
		subject: Subject{RequirementID: req, Revision: 3, Excerpt: "Tightened the limit."}}.single("bob@example.test", "https://req.example")
	if m.Subject != `Alice saved a new revision of BRK-R4 "Stopping distance"` {
		t.Errorf("the subject is %q", m.Subject)
	}
	for _, want := range []string{"Tightened the limit.", "https://req.example/r/" + req.String(), "https://req.example" + PreferencesPath} {
		if !strings.Contains(m.Body, want) {
			t.Errorf("the body lacks %q:\n%s", want, m.Body)
		}
	}
	if got := RequirementURL("https://req.example", req, Subject{ThreadID: &thread}); !strings.HasSuffix(got, "#thread-"+thread.String()) {
		t.Errorf("a thread's link is %s", got)
	}
	if got := Sentence(KindCommented, "", "BRK-R4", "Stopping distance"); got != `Somebody commented on BRK-R4 "Stopping distance"` {
		t.Errorf("a gone actor reads %q", got)
	}
	for _, k := range Kinds {
		if got := Sentence(k, "Alice", "BRK-R4", "Stopping distance"); !strings.Contains(got, "BRK-R4") || strings.HasPrefix(got, "Alice changed") {
			t.Errorf("%s has no sentence of its own: %q", k, got)
		}
	}
}

// A digest names how many updates it gathers and leads to each.
func TestADigestGathersEveryUpdate(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	m := bundle("bob@example.test", "https://req.example", []mailed{
		{kind: KindCommented, actor: "Alice", requirementID: a, key: "BRK-R1", title: "One", subject: Subject{Excerpt: "Why?"}},
		{kind: KindTransitioned, actor: "Carol", requirementID: b, key: "BRK-R2", title: "Two"},
	})
	if m.Subject != "2 updates in Exciter" {
		t.Errorf("the subject is %q", m.Subject)
	}
	for _, want := range []string{"/r/" + a.String(), "/r/" + b.String(), "Why?", PreferencesPath} {
		if !strings.Contains(m.Body, want) {
			t.Errorf("the body lacks %q:\n%s", want, m.Body)
		}
	}
}

// Mails and notifications lead to /projects/, where the suite puts a project; /p/ only redirects.
func TestAProjectsPagesAreLinkedUnderProjects(t *testing.T) {
	id := uuid.MustParse("0195f000-0000-7000-8000-0000000000a1")
	for got, want := range map[string]string{
		SessionURL("https://exciter.example", "CHK", id):   "https://exciter.example/projects/CHK/sessions/" + id.String(),
		CodeTagsURL("https://exciter.example", "CHK"):      "https://exciter.example/projects/CHK/code-tags",
		ExecutionURL("https://exciter.example", "CHK", id): "https://exciter.example/projects/CHK/tests/executions/" + id.String(),
	} {
		if got != want {
			t.Errorf("a link reads %q, want %q", got, want)
		}
	}
}
