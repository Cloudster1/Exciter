package rql

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestCompileBindsEveryValue(t *testing.T) {
	day := func(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }
	cases := []struct {
		in   string
		sql  string
		args []any
	}{
		{`owner = currentUser()`, `(r.owner_id = ANY($3::uuid[]))`, []any{[]uuid.UUID{me}}},
		{`author IN (currentUser(), "Ada L", ada@exciter.test)`,
			`((r.created_by = ANY($3::uuid[])) OR (r.created_by IN (SELECT u.id FROM app_user u WHERE lower(u.email::text) = ANY($4::text[]) OR lower(u.name) = ANY($4::text[]))))`,
			[]any{[]uuid.UUID{me}, []string{"ada l", "ada@exciter.test"}}},
		{`key = chk-r12`, `(r.id IN (requirement_key_id($3::text, $4::int)))`, []any{"CHK", int64(12)}},
		{`parent IN (CHK-R1, OLD-R2)`, `(r.parent_id IN (requirement_key_id($3::text, $4::int), requirement_key_id($5::text, $6::int)))`,
			[]any{"CHK", int64(1), "OLD", int64(2)}},
		{`title = "Pay the Basket"`, `(lower(r.title) = ANY($3::text[]))`, []any{[]string{"pay the basket"}}},
		{`title ~ basket`, `(r.title ILIKE '%' || $3 || '%')`, []any{`basket`}},
		{`text ~ "pay -card"`, `(r.search_vector @@ websearch_to_tsquery('exciter_search'::regconfig, $3))`, []any{"pay -card"}},
		{`rationale IS EMPTY`, `(COALESCE(btrim(requirement_plain_text(r.rationale)), '') = '')`, nil},
		{`kind = Section`, `(r.kind = ANY($3::text[]))`, []any{[]string{"section"}}},
		{`type IN (Feature, "Safety goal")`, `(r.type_id IN (SELECT x.id FROM requirement_type x WHERE x.project_id = r.project_id AND lower(x.name) = ANY($3::text[])))`, []any{[]string{"feature", "safety goal"}}},
		{`status != Approved`, `NOT COALESCE((r.status_id IN (SELECT x.id FROM req_status x WHERE x.project_id = r.project_id AND lower(x.name) = ANY($3::text[]))), false)`, []any{[]string{"approved"}}},
		{`category = review`, `((SELECT x.category FROM req_status x WHERE x.id = r.status_id) = ANY($3::text[]))`, []any{[]string{"review"}}},
		{`status IS EMPTY`, `(r.status_id IS NULL)`, nil},
		{`priority >= should`, `(` + priorityRank + ` >= array_position(ARRAY['wont', 'could', 'should', 'must'], $3::text))`, []any{"should"}},
		{`labels IN (Safety, ui)`, `EXISTS (SELECT 1 FROM requirement_label l WHERE l.requirement_id = r.id AND l.name = ANY($3::text[]))`, []any{[]string{"safety", "ui"}}},
		{`labels IS EMPTY`, `NOT EXISTS (SELECT 1 FROM requirement_label l WHERE l.requirement_id = r.id)`, nil},
		{`revision > 2`, `(r.revision > $3::numeric)`, []any{float64(2)}},
		{`updated >= -7d`, `(r.updated_at >= $3::timestamptz)`, []any{testNow.Add(-7 * 24 * time.Hour)}},
		{`created = 2026-09-01`, `(r.created_at >= $3::timestamptz AND r.created_at < $4::timestamptz)`, []any{day(2026, 9, 1), day(2026, 9, 2)}},
		{`created <= 2026-09-01`, `(r.created_at < $3::timestamptz)`, []any{day(2026, 9, 2)}},
		{`created > 2026-09-01T08:30`, `(r.created_at > $3::timestamptz)`, []any{time.Date(2026, 9, 1, 8, 30, 0, 0, time.UTC)}},
		{`created >= startOfWeek(-1w)`, `(r.created_at >= $3::timestamptz)`, []any{day(2026, 8, 24)}},
		{`approval = outdated`, `(` + approvalState + ` = ANY($3::text[]))`, []any{[]string{"outdated"}}},
		{`verified IN (none, partial)`, `(requirement_coverage(r.id, 'verified') = ANY($3::text[]))`, []any{[]string{"none", "partial"}}},
		{`derivesFrom = CHK-R4`,
			`EXISTS (SELECT 1 FROM trace_link tl JOIN requirement other ON tl.source_id = r.id AND other.id = tl.target_id AND other.trashed_at IS NULL WHERE tl.type_id = $5::uuid AND tl.deleted_at IS NULL AND other.id IN (requirement_key_id($3::text, $4::int)))`,
			[]any{"CHK", int64(4), derivesID}},
		{`verifiedBy IS EMPTY`,
			`NOT EXISTS (SELECT 1 FROM trace_link tl JOIN requirement other ON tl.target_id = r.id AND other.id = tl.source_id AND other.trashed_at IS NULL WHERE tl.type_id = $3::uuid AND tl.deleted_at IS NULL)`,
			[]any{verifiesID}},
		{`relatesTo IS NOT EMPTY`,
			`NOT NOT EXISTS (SELECT 1 FROM trace_link tl JOIN requirement other ON ((tl.source_id = r.id AND other.id = tl.target_id) OR (tl.target_id = r.id AND other.id = tl.source_id)) AND other.trashed_at IS NULL WHERE tl.type_id = $3::uuid AND tl.deleted_at IS NULL)`,
			[]any{relatesID}},
		{`"risk" IN (low, HIGH)`, `(r.attributes ->> $3 = ANY($4::text[]))`, []any{riskID.String(), []string{lowID.String(), highID.String()}}},
		{`"Tags" = ui`, `((r.attributes -> $3) ?| $4::text[])`, []any{tagsID.String(), []string{lowID.String()}}},
		{`"Story points" >= 3`, `(` + numeric("$3") + ` >= $4::numeric)`, []any{pointsID.String(), float64(3)}},
		{`"Due" < startOfMonth()`, `(r.attributes ->> $3 < $4::text)`, []any{dueID.String(), "2026-09-01"}},
		{`"Safety" = true`, `(r.attributes -> $3 = to_jsonb($4::boolean))`, []any{safetyID.String(), true}},
		{`"Notes" ~ "50%"`, `(COALESCE(r.attributes ->> $3, '') ILIKE '%' || $4 || '%')`, []any{notesID.String(), `50\%`}},
		{`"Detail" ~ brakes`, `(to_tsvector('exciter_search'::regconfig, COALESCE(requirement_plain_text(r.attributes -> $3), '')) @@ plainto_tsquery('exciter_search'::regconfig, $4))`,
			[]any{detailID.String(), "brakes"}},
		{`"Reviewer" IS EMPTY`, `NOT (r.attributes ? $3)`, []any{reviewerID.String()}},
	}
	for _, c := range cases {
		sql, args := compile(t, c.in).SQL(3)
		if sql != c.sql {
			t.Errorf("%q\n got %s\nwant %s", c.in, sql, c.sql)
		}
		if sprint(args) != sprint(c.args) && !(len(args) == 0 && len(c.args) == 0) {
			t.Errorf("%q: args\n got %v\nwant %v", c.in, args, c.args)
		}
	}
}

// A negation keeps the requirements with nothing there, as Armature's does: != on an owner keeps those without one.
func TestANegationKeepsWhatHasNothingThere(t *testing.T) {
	for in, want := range map[string]string{
		`owner != currentUser()`:   `NOT COALESCE((r.owner_id = ANY($1::uuid[])), false)`,
		`NOT priority = must`:      `NOT COALESCE((r.priority = ANY($1::text[])), false)`,
		`title !~ draft`:           `NOT COALESCE((r.title ILIKE '%' || $1 || '%'), false)`,
		`"Risk" NOT IN (High)`:     `NOT COALESCE((r.attributes ->> $1 = ANY($2::text[])), false)`,
		`labels IS NOT EMPTY`:      `NOT NOT EXISTS (SELECT 1 FROM requirement_label l WHERE l.requirement_id = r.id)`,
		`"Story points" != 3`:      `NOT COALESCE((` + numeric("$1") + ` = ANY($2::numeric[])), false)`,
		`verified != full`:         `NOT COALESCE((requirement_coverage(r.id, 'verified') = ANY($1::text[])), false)`,
		`rationale IS NOT EMPTY`:   `NOT (COALESCE(btrim(requirement_plain_text(r.rationale)), '') = '')`,
		`statement !~ "brake pad"`: `NOT COALESCE((to_tsvector('exciter_search'::regconfig, COALESCE(requirement_plain_text(r.statement), '')) @@ plainto_tsquery('exciter_search'::regconfig, $1)), false)`,
	} {
		if got, _ := compile(t, in).SQL(1); got != want {
			t.Errorf("%q\n got %s\nwant %s", in, got, want)
		}
	}
}

func TestCompileOrdersByTheCatalogOnly(t *testing.T) {
	compiled := compile(t, `category = draft ORDER BY priority DESC, key, status, "Risk"`)
	order := compiled.PlaceOrder(func(any) string { return "$9" })
	want := []Order{{priorityRank, true}, {"r.number", false}, {"(SELECT x.position FROM req_status x WHERE x.id = r.status_id)", false},
		{"array_position($9::text[], r.attributes ->> $9)", false}}
	if len(order) != len(want) {
		t.Fatalf("got %v", order)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Errorf("order %d: got %v want %v", i, order[i], want[i])
		}
	}
	if !compiled.Sorts() || compile(t, `kind = section`).Sorts() {
		t.Error("only a query with ORDER BY sorts")
	}
}

func TestCompileRefusesWhatTheCatalogDoesNotKnow(t *testing.T) {
	cases := []struct {
		in   string
		pos  int
		want string
	}{
		{`ownr = x`, 1, `Unknown field "ownr". Write owner if you mean that one.`},
		{`bogus IN (1)`, 1, "The fields are approval, author, category, created, decomposed, derivedInto, derivesFrom, gatedBy, gates, implemented"},
		{`priority ~ high`, 1, `"~" does not apply to priority. Use =, !=, <, <=, >, >=, IN or IS EMPTY.`},
		{`labels < a`, 1, `"<" does not apply to labels`},
		{`revision = big`, 12, "revision is a number, and big is not one"},
		{`created >= soon`, 12, "soon is not a date. Write 2026-09-04, -7d or startOfWeek()"},
		{`category = open`, 12, `"open" is not a category. Use draft, review, approved, done or closed.`},
		{`approval = maybe`, 12, `"maybe" is not a approval. Use approved, pending, outdated or none.`},
		{`priority > urgent`, 12, `"urgent" is not a priority`},
		{`ORDER BY labels`, 10, "labels cannot be sorted by"},
		{`owner = now()`, 9, "now() does not name a person"},
		{`title IS EMPTY`, 1, "title is never empty"},
		{`approval IS EMPTY`, 1, "approval is never empty"},
		{`created = tomorrow()`, 11, "There is no function tomorrow(). The date functions are now(), startOfDay()"},
		{`key = 12`, 7, `12 is not a requirement key. Write it like CHK-R12.`},
		{`derivesFrom = CHK-12`, 15, `"CHK-12" is not a requirement key`},
		{`status = now()`, 10, "now() cannot be compared with status"},
		{`title ~ ""`, 9, "Give title some text to look for"},
		{`"" = x`, 1, "An attribute's name cannot be blank"},
		{`"Rsik" = x`, 1, `No attribute is called "Rsik" here. Write "Risk" if you mean that one.`},
		{`"Colour" = x`, 1, `No attribute is called "Colour" here. The attributes are Detail, Due, Notes, Reviewer, Risk, Safety, Story points, Tags.`},
		{`"Risk" = Medium`, 10, `"Medium" is not an option of Risk. Use Low or High.`},
		{`"Risk" < High`, 1, `"<" does not apply to Risk. Use =, !=, IN or IS EMPTY.`},
		{`"Safety" = maybe`, 12, "Safety is true or false"},
		{`"Detail" = x`, 1, `"=" does not apply to Detail. Use ~, !~ or IS EMPTY.`},
		{`"Due" = soon`, 9, "Due is a date, and soon is not one"},
		{`text = login`, 1, `"=" does not apply to text. Use ~ or !~.`},
	}
	for _, c := range cases {
		q, err := Parse(c.in)
		if err != nil {
			t.Fatalf("%q: parse: %v", c.in, err)
		}
		_, err = q.Compile(sample(), Env{UserID: me, Now: testNow})
		var e *Error
		if !errors.As(err, &e) {
			t.Fatalf("%q: expected an Error, got %v", c.in, err)
		}
		if e.Pos != c.pos || !strings.Contains(e.Msg, c.want) {
			t.Errorf("%q: got %d %q, want %d containing %q", c.in, e.Pos, e.Msg, c.pos, c.want)
		}
	}
}

// A link type gives a field for each end, named after its phrase, and one for a type that reads the same both ways;
// a phrase that would take a field's name falls back to the type's key.
func TestLinkTypesNameTheirEnds(t *testing.T) {
	fields := strings.Join(sample().Fields(), " ")
	for _, want := range []string{"derivesFrom", "derivedInto", "verifies", "verifiedBy", "relatesTo", "gates", "gatedBy"} {
		if !strings.Contains(" "+fields+" ", " "+want+" ") {
			t.Errorf("%s missing from %s", want, fields)
		}
	}
	for in, want := range map[string]string{"is derived into": "derivedInto", "Conflicts with": "conflictsWith", "--": "", "is 2nd to": "", "in": ""} {
		if got := phraseName(in); got != want {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
}

// Whatever a person types is bound, never spliced: the SQL text must not contain it, and the arguments must.
func TestNothingTypedReachesTheSQL(t *testing.T) {
	hostile := []string{
		`title ~ "'; DROP TABLE requirement; --"`,
		`"Notes" = "x) OR 1=1"`,
		`labels IN ("$1", "?0")`,
		`owner = "robert'); --"`,
		`status = 'pg_sleep(10)'`,
		`text ~ "a' || pg_sleep(10) || '"`,
	}
	for _, in := range hostile {
		sql, args := compile(t, in).SQL(1)
		for _, arg := range args {
			text := sprint(arg)
			for _, piece := range []string{"DROP", "OR 1=1", "pg_sleep", "--", "?0"} {
				if strings.Contains(text, piece) && strings.Contains(sql, piece) {
					t.Errorf("%q: %q reached the SQL: %s", in, piece, sql)
				}
			}
		}
		if placeholder.MatchString(sql) {
			t.Errorf("%q: a placeholder was left unnumbered: %s", in, sql)
		}
		if len(args) == 0 {
			t.Errorf("%q: nothing was bound", in)
		}
	}
}

func TestSQLNumbersFromWhereItIsPlaced(t *testing.T) {
	compiled := compile(t, `owner = currentUser() AND labels = a`)
	first, _ := compiled.SQL(1)
	seventh, _ := compiled.SQL(7)
	if !strings.Contains(first, "$1") || !strings.Contains(first, "$2") {
		t.Errorf("from 1: %s", first)
	}
	if !strings.Contains(seventh, "$7") || !strings.Contains(seventh, "$8") || strings.Contains(seventh, "$1") {
		t.Errorf("from 7: %s", seventh)
	}
	if sql, args := (*Compiled)(nil).SQL(1); sql != "" || args != nil {
		t.Errorf("a nil query is no clause")
	}
	// A value used twice is bound once.
	if _, args := compile(t, `owner IN (ada)`).SQL(1); len(args) != 1 {
		t.Errorf("got %v", args)
	}
}

// A text search looks for what was typed: % and _ are characters a title can hold, so they reach ILIKE escaped and
// never as wildcards that match anything.
func TestTextSearchTakesWildcardsLiterally(t *testing.T) {
	cases := []struct {
		in   string
		args []any
	}{
		{`title ~ "50%"`, []any{`50\%`}},
		{`title ~ snake_case`, []any{`snake\_case`}},
		{`title ~ "C:\\temp"`, []any{`C:\\temp`}},
		{`title !~ "100%_done\\"`, []any{`100\%\_done\\`}},
		// Words are matched by the search index, not by a pattern, so they stay as typed.
		{`text ~ "50%"`, []any{`50%`}},
	}
	for _, c := range cases {
		_, args := compile(t, c.in).SQL(1)
		if sprint(args) != sprint(c.args) {
			t.Errorf("%q: got %v want %v", c.in, args, c.args)
		}
	}
}

func TestDistance(t *testing.T) {
	if d := distance("ownr", "owner"); d != 1 {
		t.Errorf("got %d", d)
	}
	if nearest("zzzzzz", sample().Fields()) != "" {
		t.Errorf("nothing is near zzzzzz")
	}
	if got := nearest("Statuss", sample().Fields()); got != "status" {
		t.Errorf("got %q", got)
	}
}
