//go:build integration

package test

import (
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// The query language (#47): one line narrows the grid with its words and controls, may order it, narrows what reads
// the grid's filter, and a query that cannot run says where.

// badQuery fails unless the answer refuses a query at the character given, with a sentence holding message.
func badQuery(t *testing.T, what string, got response, position int, message string) {
	t.Helper()
	refuse(t, what, got, http.StatusBadRequest, "bad_query")
	e, _ := got.Body["error"].(map[string]any)
	if e["position"] != float64(position) || !strings.Contains(e["message"].(string), message) {
		t.Errorf("%s: got %v, want position %d and %q", what, e, position, message)
	}
}

func TestTheGridTakesAQuery(t *testing.T) {
	key := projectKey(t, "QRY")
	f := newSearchFixture(t, key)
	r := func(n ...string) []string {
		out := make([]string, len(n))
		for i, each := range n {
			out[i] = key + "-R" + each
		}
		return out
	}
	grid := func(c *client, query, more string) []string {
		t.Helper()
		return rowKeys(t, want(t, c.get(t, "/api/v1/projects/"+key+"/requirements?query="+url.QueryEscape(query)+more), http.StatusOK, "the grid by "+query))
	}

	t.Run("fields, attributes by name and negations that keep what has nothing", func(t *testing.T) {
		for query, keys := range map[string][]string{
			`kind = section`:                          r("3"),
			`title ~ receipt`:                         r("3", "4", "5"),
			`text ~ receipt`:                          r("4", "5"),
			`statement ~ card`:                        r("2"),
			`rationale IS NOT EMPTY`:                  r("2"),
			`priority >= could`:                       r("1", "2"),
			`priority IS EMPTY`:                       r("3", "4", "5"),
			`labels = CHECKOUT`:                       r("1", "4"),
			`labels IS EMPTY`:                         r("2", "3", "5"),
			`key IN (` + key + `-R4, ` + key + `-R1)`: r("1", "4"),
			`"Risk" = high`:                           r("1"),
			`"risk" != High`:                          r("2", "3", "4", "5"),
			`"Cost" >= 6`:                             r("2"),
			`"Cost" <= 5 AND "Due" >= 2026-01-01`:     r("1"),
			`"Note" = "card OR cash"`:                 r("1"),
			`"Detail" ~ creme`:                        r("2"),
			`"Due" IS EMPTY AND kind = requirement`:   r("2", "4", "5"),
			`author = currentUser()`:                  r("1", "2", "3", "4", "5"),
			`owner IS EMPTY AND revision = 1`:         r("1", "2", "3", "4", "5"),
			`created < 2020-01-01 OR updated < -1d`:   nil,
			`NOT (title ~ pay OR title ~ refund)`:     r("3", "4", "5"),
			`title ~ "'; DROP TABLE requirement; --"`: nil,
		} {
			sameList(t, query, grid(f.plain, query, ""), keys...)
		}
	})

	t.Run("a query ands with the words and controls, and its order outranks the column", func(t *testing.T) {
		sameList(t, "with a control", grid(f.plain, `labels = checkout`, "&priority=must"), r("1")...)
		sameList(t, "with words", grid(f.plain, `kind = requirement`, "&q=receipt"), r("4", "5")...)
		sameList(t, "by an attribute, empty last", grid(f.plain, `ORDER BY "Cost" DESC`, ""), r("2", "1", "3", "4", "5")...)
		sameList(t, "its order over the column", grid(f.plain, `kind = requirement ORDER BY title DESC`, "&sort=key"), r("2", "4", "1", "5")...)
		sameList(t, "by priority, highest first", grid(f.plain, `priority IS NOT EMPTY ORDER BY priority DESC`, ""), r("1", "2")...)
	})

	t.Run("a query that cannot run says where", func(t *testing.T) {
		at := func(query string) response {
			return f.plain.get(t, "/api/v1/projects/"+key+"/requirements?query="+url.QueryEscape(query))
		}
		badQuery(t, "a field misspelled", at(`ownr = x`), 1, `Write owner if you mean that one.`)
		badQuery(t, "a value missing", at(`title =`), 8, "ends where a value was expected")
		badQuery(t, "an option the attribute lacks", at(`"Risk" = Medium`), 10, `"Medium" is not an option of Risk. Use Low or High.`)
		badQuery(t, "an attribute the project lacks", at(`"Colour" = red`), 1, `No attribute is called "Colour" here`)
		badQuery(t, "too long", at(strings.Repeat("a", 2001)), 2001, "Keep the query to 2000 characters")
		badQuery(t, "by the export", f.plain.get(t, "/api/v1/projects/"+key+"/export.csv?query="+url.QueryEscape(`ownr = x`)), 1, "owner")
		badQuery(t, "by the coverage", f.plain.get(t, "/api/v1/projects/"+key+"/coverage?query="+url.QueryEscape(`ownr = x`)), 1, "owner")
		badQuery(t, "by the matrix", f.plain.get(t, "/api/v1/projects/"+key+"/matrix?query="+url.QueryEscape(`ownr = x`)), 1, "owner")
		badQuery(t, "by the matrix's file", f.plain.get(t, "/api/v1/projects/"+key+"/matrix.csv?query="+url.QueryEscape(`ownr = x`)), 1, "owner")
	})

	t.Run("what reads the grid's filter narrows by the query", func(t *testing.T) {
		file := want(t, f.plain.get(t, "/api/v1/projects/"+key+"/export.csv?query="+url.QueryEscape(`labels = checkout`)), http.StatusOK, "the export")
		if lines := strings.Split(strings.TrimSpace(string(file.Raw)), "\n"); len(lines) != 3 || !strings.Contains(string(file.Raw), key+"-R4") {
			t.Errorf("the export holds the header and two rows: %s", file.Raw)
		}
		covered := want(t, f.plain.get(t, "/api/v1/projects/"+key+"/coverage?query="+url.QueryEscape(`title ~ receipt`)), http.StatusOK, "the coverage")
		if covered.Body["total"] != float64(2) {
			t.Errorf("the coverage keeps two: %v", covered.Body)
		}
		m := obj(t, want(t, f.plain.get(t, "/api/v1/projects/"+key+"/matrix?query="+url.QueryEscape(`"Risk" IS NOT EMPTY`)), http.StatusOK, "the matrix"), "matrix")
		if m["total"] != float64(2) {
			t.Errorf("the matrix keeps two: %v", m)
		}
	})

	t.Run("a view keeps its query, and refuses one that cannot run", func(t *testing.T) {
		saved := obj(t, want(t, f.plain.post(t, "/api/v1/projects/"+key+"/views", map[string]any{"name": "Risky",
			"filter": map[string]any{"query": `"Risk" = High ORDER BY title`}}), http.StatusCreated, "save a view with a query"), "view")
		if saved["filter"].(map[string]any)["query"] != `"Risk" = High ORDER BY title` {
			t.Errorf("the view keeps the query: %v", saved)
		}
		badQuery(t, "a view's query misspelled", f.plain.post(t, "/api/v1/projects/"+key+"/views", map[string]any{"name": "Bad",
			"filter": map[string]any{"query": `ownr = x`}}), 1, "owner")
		badQuery(t, "a view changed to a query that cannot run", f.plain.patch(t, "/api/v1/views/"+saved["id"].(string),
			map[string]any{"filter": map[string]any{"query": `title =`}}), 8, "value")
	})

	t.Run("the box completes fields, values and names, and finds requirements", func(t *testing.T) {
		suggest := func(q string, at ...int) map[string]any {
			t.Helper()
			path := "/api/v1/projects/" + key + "/requirements/suggest?q=" + url.QueryEscape(q)
			if len(at) > 0 {
				path += "&at=" + strconv.Itoa(at[0])
			}
			return want(t, f.plain.get(t, path), http.StatusOK, "suggest "+q).Body
		}
		words := func(s map[string]any) map[string]string {
			out := map[string]string{}
			for _, w := range s["words"].([]any) {
				word := w.(map[string]any)
				out[word["text"].(string)] = word["detail"].(string)
			}
			return out
		}
		found := func(s map[string]any) []string {
			var keys []string
			for _, h := range s["requirements"].([]any) {
				keys = append(keys, h.(map[string]any)["requirement"].(map[string]any)["key"].(string))
			}
			sort.Strings(keys)
			return keys
		}
		if w := words(suggest("sta")); w["status"] != "field" || w["statement"] != "field" {
			t.Errorf("a field half typed: %v", w)
		}
		if w := words(suggest(`Ri`)); w[`"Risk"`] != "attribute" {
			t.Errorf("an attribute by its quoted name: %v", w)
		}
		if w := words(suggest(`"Risk" = `)); w["High"] != "value" || w["Low"] != "value" {
			t.Errorf("an attribute's options: %v", w)
		}
		if w := words(suggest(`status = `)); w["Draft"] != "status" {
			t.Errorf("the project's statuses: %v", w)
		}
		if w := words(suggest(`labels = pa`)); w["paper"] != "label" || w["checkout"] != "" {
			t.Errorf("the project's labels: %v", w)
		}
		if w := words(suggest(`parent = ` + key + `-R`)); w[key+"-R1"] != "requirement" {
			t.Errorf("requirements by key: %v", w)
		}
		if c := suggest(`title ~ receipt`, 3)["completion"].(map[string]any); c["slot"] != "field" || c["prefix"] != "tit" {
			t.Errorf("the caret in the middle completes what is before it: %v", c)
		}
		sameList(t, "a query that runs finds what it keeps", found(suggest(`title ~ receipt`)), r("3", "4", "5")...)
		sameList(t, "words find by title", found(suggest(`Refund`)), r("2")...)
		sameList(t, "a query half typed finds nothing yet", found(suggest(`title ~`)))
		fieldError(t, f.plain.get(t, "/api/v1/projects/"+key+"/requirements/suggest?q=x&at=-1"), "at")
		fieldError(t, f.plain.get(t, "/api/v1/projects/"+key+"/requirements/suggest?q=x&limit=21"), "limit")
		refuse(t, "a guest of the installation", f.guest.get(t, "/api/v1/projects/"+key+"/requirements/suggest?q=x"), http.StatusNotFound, "not_found")
	})
	t.Run("straight through SQL, a key and a coverage level reach only what the reader sees", func(t *testing.T) {
		hidden := projectKey(t, "HID")
		var id *uuid.UUID
		if err := f.h.appConn(t, f.home.user).QueryRow(t.Context(), `SELECT requirement_key_id($1, 1)`, hidden).Scan(&id); err != nil || id == nil {
			t.Fatalf("its maker finds the hidden requirement by key: %v %v", id, err)
		}
		stranger := f.h.appConn(t, f.away.user)
		var found *uuid.UUID
		if err := stranger.QueryRow(t.Context(), `SELECT requirement_key_id($1, 1)`, hidden).Scan(&found); err != nil || found != nil {
			t.Errorf("a stranger found %v by its key: %v", found, err)
		}
		var level *string
		if err := stranger.QueryRow(t.Context(), `SELECT requirement_coverage($1, 'verified')`, *id).Scan(&level); err != nil || level != nil {
			t.Errorf("a stranger read the coverage %v of a requirement they may not see: %v", level, err)
		}
	})
}
