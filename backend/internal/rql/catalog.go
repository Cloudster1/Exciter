package rql

import (
	"sort"
	"strings"
	"unicode"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/attribute"
	"github.com/cloudster1/exciter/backend/internal/requirement"
)

// fieldKind decides how a field's values are read and what SQL they become.
type fieldKind int

const (
	fkKey fieldKind = iota
	fkTitle
	fkText
	fkDocument
	fkEnum
	fkPriority
	fkName
	fkUser
	fkLabels
	fkParent
	fkNumber
	fkTimestamp
	fkLink
	fkAttribute
)

// Link directions: the requirement is the link's source, its target, or either end of a link that reads the same
// both ways.
const (
	dirForward  = "forward"
	dirBackward = "backward"
	dirEither   = "either"
)

// fieldDef is one row of a catalog. Every piece of SQL a query can produce is spelled out here or in the compiler,
// over the requirement aliased r, which is what makes the compiler safe to point at text a stranger typed.
type fieldDef struct {
	name string
	kind fieldKind
	// column is the expression the kind compares.
	column string
	// table is where a name is looked up, for fkName.
	table string
	// enum lists the values of an enum, in the order they rank for fkPriority.
	enum []string
	// order is the ORDER BY expression; empty means the field cannot be sorted by.
	order string
	// empty says IS EMPTY applies.
	empty bool
	// link and dir say which links a link field follows, and from which end.
	link uuid.UUID
	dir  string
	// attr is the attribute a quoted name found.
	attr *attribute.Def
}

// Approval states (#35): a review waits, the current revision is approved, an earlier one was, or none ever was.
var approvalStates = []string{"approved", "pending", "outdated", "none"}

// approvalState is a requirement's approval state, by the reviews it has.
const approvalState = `(CASE
  WHEN EXISTS (SELECT 1 FROM requirement_review v WHERE v.requirement_id = r.id AND v.state = 'pending') THEN 'pending'
  WHEN r.revision = (SELECT max(v.revision) FROM requirement_review v WHERE v.requirement_id = r.id AND v.state = 'approved') THEN 'approved'
  WHEN EXISTS (SELECT 1 FROM requirement_review v WHERE v.requirement_id = r.id AND v.state = 'approved') THEN 'outdated'
  ELSE 'none' END)`

// Reuse states (#49): a copy that follows its library source, one whose source has moved on, a variant with words
// of its own, or none. A reader who may not read the source never sees it move on.
var reuseStates = []string{"following", "behind", "variant", "none"}

const reuseState = `(CASE
  WHEN r.reuse_source IS NULL THEN 'none'
  WHEN r.reuse_state = 'variant' THEN 'variant'
  WHEN COALESCE((SELECT s.revision FROM requirement s WHERE s.id = r.reuse_source), r.reuse_revision) > r.reuse_revision THEN 'behind'
  ELSE 'following' END)`

// coverageLevels are the levels of trace.Level, repeated because trace imports search, which imports this.
var coverageLevels = []string{"none", "partial", "full", "na"}

// testOutcomes are a requirement's latest results summed up, worst first, as testresult.Outcomes and none (#200).
var testOutcomes = []string{"errored", "failed", "skipped", "passed", "none"}

// testOutdated counts a requirement's tests whose latest result was made against an older revision.
const testOutdated = "(SELECT c.tests_outdated FROM requirement_test_counts(r.id) c)"

// priorityRank ranks a priority so that must is the highest, as the API orders them.
const priorityRank = `array_position(ARRAY['wont', 'could', 'should', 'must'], r.priority)`

// fixed are the fields every project has.
var fixed = []fieldDef{
	{name: "key", kind: fkKey, order: "r.number"},
	{name: "title", kind: fkTitle, column: "r.title", order: "lower(r.title)"},
	{name: "text", kind: fkText},
	{name: "statement", kind: fkDocument, column: "r.statement", empty: true},
	{name: "rationale", kind: fkDocument, column: "r.rationale", empty: true},
	{name: "kind", kind: fkEnum, column: "r.kind", enum: []string{string(requirement.KindRequirement), string(requirement.KindSection)}, order: "r.kind"},
	{name: "type", kind: fkName, column: "r.type_id", table: "requirement_type", empty: true,
		order: "(SELECT lower(x.name) FROM requirement_type x WHERE x.id = r.type_id)"},
	{name: "status", kind: fkName, column: "r.status_id", table: "req_status", empty: true,
		order: "(SELECT x.position FROM req_status x WHERE x.id = r.status_id)"},
	{name: "category", kind: fkEnum, column: "(SELECT x.category FROM req_status x WHERE x.id = r.status_id)", empty: true,
		enum: []string{"draft", "review", "approved", "done", "closed"}},
	{name: "owner", kind: fkUser, column: "r.owner_id", empty: true, order: "(SELECT lower(u.name) FROM app_user u WHERE u.id = r.owner_id)"},
	{name: "author", kind: fkUser, column: "r.created_by", order: "(SELECT lower(u.name) FROM app_user u WHERE u.id = r.created_by)"},
	{name: "priority", kind: fkPriority, column: "r.priority", enum: []string{"wont", "could", "should", "must"}, empty: true, order: priorityRank},
	{name: "labels", kind: fkLabels, empty: true},
	{name: "parent", kind: fkParent, column: "r.parent_id", empty: true},
	{name: "revision", kind: fkNumber, column: "r.revision", order: "r.revision"},
	{name: "created", kind: fkTimestamp, column: "r.created_at", order: "r.created_at"},
	{name: "updated", kind: fkTimestamp, column: "r.updated_at", order: "r.updated_at"},
	{name: "suspect", kind: fkNumber, column: requirement.SuspectLinks, order: requirement.SuspectLinks},
	{name: "approval", kind: fkEnum, column: approvalState, enum: approvalStates},
	{name: "reuse", kind: fkEnum, column: reuseState, enum: reuseStates},
	{name: "decomposed", kind: fkEnum, column: "requirement_coverage(r.id, 'decomposed')", enum: coverageLevels},
	{name: "implemented", kind: fkEnum, column: "requirement_coverage(r.id, 'implemented')", enum: coverageLevels},
	{name: "verified", kind: fkEnum, column: "requirement_coverage(r.id, 'verified')", enum: coverageLevels},
	{name: "testResult", kind: fkEnum, column: "requirement_test_result(r.id)", enum: testOutcomes},
	{name: "testOutdated", kind: fkNumber, column: testOutdated, order: testOutdated},
}

// LinkType is a link type a project's requirements may be joined by, with the phrases its ends read in.
type LinkType struct {
	ID       uuid.UUID
	Key      string
	Forward  string
	Backward string
}

// Catalog is what a query may say in one project: the fixed fields, a field for each end of its link types, and its
// attributes by their quoted names.
type Catalog struct {
	fields     []fieldDef
	attributes []attribute.Def
}

// NewCatalog builds a project's catalog. A link type gives a field for each end, named after its phrase, so that
// "is verified by" is verifiedBy; one that reads the same both ways gives one field for either end.
func NewCatalog(attributes []attribute.Def, links []LinkType) *Catalog {
	c := &Catalog{fields: append([]fieldDef{}, fixed...), attributes: attributes}
	taken := map[string]bool{}
	for _, def := range fixed {
		taken[strings.ToLower(def.name)] = true
	}
	name := func(candidates ...string) string {
		for _, n := range candidates {
			if n != "" && !taken[strings.ToLower(n)] {
				taken[strings.ToLower(n)] = true
				return n
			}
		}
		return ""
	}
	for _, lt := range links {
		forward, backward := phraseName(lt.Forward), phraseName(lt.Backward)
		if strings.EqualFold(strings.TrimSpace(lt.Forward), strings.TrimSpace(lt.Backward)) {
			if n := name(forward, lt.Key); n != "" {
				c.fields = append(c.fields, fieldDef{name: n, kind: fkLink, link: lt.ID, dir: dirEither, empty: true})
			}
			continue
		}
		if n := name(forward, lt.Key); n != "" {
			c.fields = append(c.fields, fieldDef{name: n, kind: fkLink, link: lt.ID, dir: dirForward, empty: true})
		}
		if n := name(backward, lt.Key+"By"); n != "" {
			c.fields = append(c.fields, fieldDef{name: n, kind: fkLink, link: lt.ID, dir: dirBackward, empty: true})
		}
	}
	return c
}

// phraseName makes a field name of a link phrase: "is derived into" is derivedInto. A phrase with nothing a field
// name can hold gives none.
func phraseName(phrase string) string {
	words := strings.FieldsFunc(phrase, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	if len(words) > 1 && (strings.EqualFold(words[0], "is") || strings.EqualFold(words[0], "are")) {
		words = words[1:]
	}
	if len(words) == 0 || !unicode.IsLetter([]rune(words[0])[0]) {
		return ""
	}
	var b strings.Builder
	for i, w := range words {
		w = strings.ToLower(w)
		if i > 0 {
			r := []rune(w)
			r[0] = unicode.ToUpper(r[0])
			w = string(r)
		}
		b.WriteString(w)
	}
	if keywords[strings.ToUpper(b.String())] {
		return ""
	}
	return b.String()
}

// The operator sets. "in" and "empty" stand for IN and IS EMPTY.
var (
	opsEquality = []string{"=", "!=", "in"}
	opsOrdered  = []string{"=", "!=", "<", "<=", ">", ">=", "in"}
	opsText     = []string{"=", "!=", "~", "!~", "in"}
	opsContains = []string{"~", "!~"}
)

// operators lists what a field takes.
func (d fieldDef) operators() []string {
	var ops []string
	switch d.kind {
	case fkKey, fkEnum, fkName, fkUser, fkLabels, fkParent, fkLink:
		ops = opsEquality
	case fkTitle:
		ops = opsText
	case fkText, fkDocument:
		ops = opsContains
	case fkPriority, fkNumber, fkTimestamp:
		ops = opsOrdered
	case fkAttribute:
		switch d.attr.Kind {
		case attribute.Text, attribute.URL:
			ops = opsText
		case attribute.RichText:
			ops = opsContains
		case attribute.Number, attribute.Date:
			ops = opsOrdered
		case attribute.Boolean:
			ops = []string{"=", "!="}
		default:
			ops = opsEquality
		}
	}
	if d.empty || d.kind == fkAttribute {
		return append(append([]string{}, ops...), "empty")
	}
	return ops
}

func (d fieldDef) allows(op string) bool {
	for _, each := range d.operators() {
		if each == op {
			return true
		}
	}
	return false
}

// describeOperators words an operator list for an error message.
func describeOperators(ops []string) string {
	words := make([]string, 0, len(ops))
	for _, op := range ops {
		switch op {
		case "in":
			words = append(words, "IN")
		case "empty":
			words = append(words, "IS EMPTY")
		default:
			words = append(words, op)
		}
	}
	if len(words) == 1 {
		return words[0]
	}
	return strings.Join(words[:len(words)-1], ", ") + " or " + words[len(words)-1]
}

// lookup finds a field by name, case-insensitively. A quoted name is always an attribute's, so that an attribute
// called "status" is still reachable.
func (c *Catalog) lookup(f Field) (fieldDef, error) {
	if f.Custom {
		if strings.TrimSpace(f.Name) == "" {
			return fieldDef{}, errAt(f.Pos, "An attribute's name cannot be blank.")
		}
		for i := range c.attributes {
			if strings.EqualFold(strings.TrimSpace(c.attributes[i].Name), strings.TrimSpace(f.Name)) {
				a := &c.attributes[i]
				return fieldDef{name: a.Name, kind: fkAttribute, attr: a, empty: true}, nil
			}
		}
		if near := nearest(f.Name, c.Attributes()); near != "" {
			return fieldDef{}, errAt(f.Pos, "No attribute is called %q here. Write \"%s\" if you mean that one.", f.Name, near)
		}
		if len(c.attributes) == 0 {
			return fieldDef{}, errAt(f.Pos, "No attribute is called %q here; this project has no attributes.", f.Name)
		}
		return fieldDef{}, errAt(f.Pos, "No attribute is called %q here. The attributes are %s.", f.Name, strings.Join(c.Attributes(), ", "))
	}
	for _, def := range c.fields {
		if strings.EqualFold(def.name, f.Name) {
			return def, nil
		}
	}
	if near := nearest(f.Name, c.Fields()); near != "" {
		return fieldDef{}, errAt(f.Pos, "Unknown field %q. Write %s if you mean that one.", f.Name, near)
	}
	return fieldDef{}, errAt(f.Pos, "Unknown field %q. The fields are %s; quote an attribute's name.", f.Name, strings.Join(c.Fields(), ", "))
}

// Fields lists the field names, sorted, for messages and completion.
func (c *Catalog) Fields() []string {
	out := make([]string, len(c.fields))
	for i, def := range c.fields {
		out[i] = def.name
	}
	sort.Strings(out)
	return out
}

// Attributes lists the attributes' names, sorted.
func (c *Catalog) Attributes() []string {
	out := make([]string, len(c.attributes))
	for i, a := range c.attributes {
		out[i] = a.Name
	}
	sort.Strings(out)
	return out
}

// maxSuggestionDistance is how far a misspelling may be from a name and still get that name suggested.
const maxSuggestionDistance = 2

func nearest(name string, names []string) string {
	best, bestDistance := "", maxSuggestionDistance+1
	for _, n := range names {
		d := distance(strings.ToLower(name), strings.ToLower(n))
		if d < bestDistance {
			best, bestDistance = n, d
		}
	}
	return best
}

// distance is the Levenshtein distance between two short strings.
func distance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur := make([]int, len(rb)+1)
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(rb)]
}
