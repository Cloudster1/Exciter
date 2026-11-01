package demo

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/cloudster1/exciter/backend/internal/requirement"
)

// Each tool's specification (#488) is written by hand, a file per part, and travels with the binary as the snapshot
// does.
//
//go:embed spec/*/*.md
var specFiles embed.FS

// The item types a specification names, as the demo's projects call them.
const (
	TypeStakeholderNeed = "Stakeholder need"
	TypeFunctional      = "Functional"
	TypeNonFunctional   = "Non-functional"
	TypeInterface       = "Interface"
	TypeUseCase         = "Use case"
	TypeTestCase        = "Test case"
)

// itemTypes are the types an item may name, in the order the format lists them.
var itemTypes = []string{TypeStakeholderNeed, TypeFunctional, TypeNonFunctional, TypeInterface, TypeUseCase, TypeTestCase}

// The values the fields take.
var (
	priorities    = []string{"must", "should", "could", "wont"}
	verifications = []string{"Test", "Analysis", "Inspection", "Demonstration"}
	results       = []string{"passed", "failed", "blocked", "skipped"}
	yesNo         = []string{"yes", "no"}
)

// The paragraphs of a body the format gives a meaning.
const (
	leadRationale     = "Rationale:"
	leadPreconditions = "Preconditions:"
	leadParameters    = "Parameters:"
	pathSeparator     = " / "
	headFile          = "# "
	headSection       = "## "
	headItem          = "### "
	columnAction      = "Action"
	columnExpected    = "Expected"
)

var (
	specID     = regexp.MustCompile(`^[A-Z][A-Z0-9]*(-[A-Z0-9]+)*$`)
	issueKey   = regexp.MustCompile(`^[A-Z]{3}-[1-9][0-9]*$`)
	guideName  = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	fieldLine  = regexp.MustCompile(`^([a-z]+):(?: (.*))?$`)
	tableRule  = regexp.MustCompile(`^:?-+:?$`)
	fieldNames = []string{"type", "priority", "verification", "component", "refines", "relates", "issues", "guide",
		"verifies", "automated", "result", "note"}
	// testCaseFields are the fields only a test case takes.
	testCaseFields = []string{"verifies", "automated", "result", "note"}
)

// SpecFile is one file of a specification: its sections, the first the one its heading names.
type SpecFile struct {
	Name     string
	Sections []SpecSection
}

// SpecSection is a section a file fills: its titles from the top, its introduction and its items.
type SpecSection struct {
	Path  []string
	Intro string
	Items []SpecItem
}

// SpecItem is a requirement or a test case as its file writes it; Line is where its heading stands.
type SpecItem struct {
	ID, Title, Type string
	Line            int
	Priority        string
	Verification    string
	Component       string
	Refines         []string
	Relates         []string
	Verifies        []string
	Issues          []string
	Guide           string
	Automated       string
	Result          string
	Note            string
	Statement       string
	Rationale       string
	Preconditions   string
	Steps           []SpecStep
	Parameters      []requirement.Parameter
}

// SpecStep is one row of a test case's table.
type SpecStep struct {
	Action, Expected string
}

// IsTestCase says the item is a test case.
func (it SpecItem) IsTestCase() bool { return it.Type == TypeTestCase }

// IsRequirement says the item is a requirement a test case should verify: neither a need nor a test case.
func (it SpecItem) IsRequirement() bool {
	return it.Type != TypeStakeholderNeed && it.Type != TypeTestCase
}

// SpecError is one thing a file gets wrong, where it stands and what to do about it.
type SpecError struct {
	File string
	Line int
	ID   string
	Text string
}

func (e *SpecError) Error() string {
	where := fmt.Sprintf("%s:%d", e.File, e.Line)
	if e.ID != "" {
		where += ": " + e.ID
	}
	return where + ": " + e.Text
}

// specParser reads one file a line at a time, collecting every mistake rather than stopping at the first.
type specParser struct {
	file    string
	out     SpecFile
	errs    []error
	item    *SpecItem
	fields  bool
	seen    map[string]bool
	body    []string
	intro   []string
	inFence bool
}

func (p *specParser) fail(line int, id, format string, args ...any) {
	p.errs = append(p.errs, &SpecError{File: p.file, Line: line, ID: id, Text: fmt.Sprintf(format, args...)})
}

func (p *specParser) section() *SpecSection { return &p.out.Sections[len(p.out.Sections)-1] }

// ParseSpec reads a file of spec/<tool>/: a section path, its introduction, then requirements and test cases under
// third-level headings and subsections under second-level ones. A heading inside a fence belongs to the fence.
func ParseSpec(file, md string) (SpecFile, error) {
	p := &specParser{file: file, out: SpecFile{Name: path.Base(file)}}
	lines := strings.Split(strings.ReplaceAll(md, "\r\n", "\n"), "\n")
	if len(lines) == 0 || !strings.HasPrefix(lines[0], headFile) {
		p.fail(1, "", "the first line is the section path, such as `# Functional requirements / Boards`. Add it.")
		return p.out, errors.Join(p.errs...)
	}
	p.out.Sections = []SpecSection{{Path: p.path(1, strings.TrimPrefix(lines[0], headFile))}}
	for i, line := range lines[1:] {
		p.line(i+2, strings.TrimRight(line, " \t"))
	}
	p.flush()
	if p.inFence {
		p.fail(len(lines), "", "a code fence is left open. Close it with ```.")
	}
	return p.out, errors.Join(p.errs...)
}

func (p *specParser) path(line int, raw string) []string {
	var out []string
	for _, title := range strings.Split(raw, pathSeparator) {
		if title = strings.TrimSpace(title); title == "" {
			p.fail(line, "", "the section path %q has an empty title. Name every section between the slashes.", raw)
			continue
		}
		out = append(out, title)
	}
	return out
}

func (p *specParser) line(n int, line string) {
	switch {
	case strings.HasPrefix(line, "```"):
		p.inFence = !p.inFence
	case p.inFence:
	case strings.HasPrefix(line, headItem):
		p.flush()
		p.start(n, strings.TrimSpace(strings.TrimPrefix(line, headItem)))
		return
	case strings.HasPrefix(line, headSection):
		p.flush()
		title := strings.TrimSpace(strings.TrimPrefix(line, headSection))
		if title == "" {
			p.fail(n, "", "a subsection heading has no title. Write `## <title>`.")
		}
		head := p.out.Sections[0].Path
		p.out.Sections = append(p.out.Sections, SpecSection{Path: append(slices.Clip(head), title)})
		return
	case strings.HasPrefix(line, headFile):
		p.fail(n, "", "only the first line is a section path. Make this `## %s` for a subsection, or start a file of its own.",
			strings.TrimPrefix(line, headFile))
		return
	case p.item != nil && p.fields:
		if line == "" {
			p.fields = false
		} else {
			p.field(n, line)
		}
		return
	}
	if p.item != nil {
		p.body = append(p.body, line)
	} else {
		p.intro = append(p.intro, line)
	}
}

func (p *specParser) start(n int, heading string) {
	id, title, _ := strings.Cut(heading, " ")
	title = strings.TrimSpace(title)
	switch {
	case !specID.MatchString(id):
		p.fail(n, id, "the heading starts with %q, which is no ID. Write `### <ID> <Title>`, the ID like FR-BRD-1.", id)
	case title == "":
		p.fail(n, id, "the heading has no title. Write `### %s <Title>`.", id)
	}
	p.item = &SpecItem{ID: id, Title: title, Line: n}
	p.fields, p.seen, p.body = true, map[string]bool{}, nil
}

func (p *specParser) field(n int, line string) {
	it := p.item
	m := fieldLine.FindStringSubmatch(line)
	if m == nil || !slices.Contains(fieldNames, m[1]) {
		p.fail(n, it.ID, "%q is not a field. Fields are %s, one `name: value` a line; leave a blank line before the statement.",
			line, strings.Join(fieldNames, ", "))
		return
	}
	name, value := m[1], strings.TrimSpace(m[2])
	if p.seen[name] {
		p.fail(n, it.ID, "%s is given twice. Keep one line of it.", name)
		return
	}
	p.seen[name] = true
	if value == "" {
		p.fail(n, it.ID, "%s has no value. Give one, or leave the line out.", name)
		return
	}
	oneOf := func(dst *string, allowed []string) {
		if !slices.Contains(allowed, value) {
			p.fail(n, it.ID, "%s is %q. Write one of %s.", name, value, strings.Join(allowed, ", "))
		}
		*dst = value
	}
	switch name {
	case "type":
		oneOf(&it.Type, itemTypes)
	case "priority":
		oneOf(&it.Priority, priorities)
	case "verification":
		oneOf(&it.Verification, verifications)
	case "automated":
		oneOf(&it.Automated, yesNo)
	case "result":
		oneOf(&it.Result, results)
	case "component":
		it.Component = value
	case "note":
		it.Note = value
	case "guide":
		it.Guide = value
		if _, _, err := GuideLabel("", value); err != nil {
			p.fail(n, it.ID, "%v", err)
		}
	case "refines", "relates", "verifies", "issues":
		pattern, example := specID, "SN-3"
		if name == "issues" {
			pattern, example = issueKey, "ARM-112"
		}
		var list []string
		for _, v := range strings.Split(value, ",") {
			if v = strings.TrimSpace(v); !pattern.MatchString(v) {
				p.fail(n, it.ID, "%s names %q. Name each like %s, separated by commas.", name, v, example)
				continue
			}
			list = append(list, v)
		}
		*map[string]*[]string{"refines": &it.Refines, "relates": &it.Relates, "verifies": &it.Verifies, "issues": &it.Issues}[name] = list
	}
}

// flush ends the item or the introduction being read.
func (p *specParser) flush() {
	if p.item == nil {
		sec := p.section()
		sec.Intro = strings.TrimSpace(strings.Join(p.intro, "\n"))
		p.intro = nil
		return
	}
	it := p.item
	p.item = nil
	p.body = trimBlank(p.body)
	p.readBody(it)
	p.check(it)
	sec := p.section()
	sec.Items = append(sec.Items, *it)
}

// trimBlank drops the blank lines a body starts and ends with.
func trimBlank(lines []string) []string {
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// blocks splits a body into its paragraphs, a fence kept whole however many blank lines it holds.
func blocks(lines []string) [][]string {
	var (
		out     [][]string
		cur     []string
		inFence bool
	)
	for _, line := range lines {
		if strings.HasPrefix(line, "```") {
			inFence = !inFence
		}
		if !inFence && strings.TrimSpace(line) == "" {
			if len(cur) > 0 {
				out = append(out, cur)
			}
			cur = nil
			continue
		}
		cur = append(cur, line)
	}
	if len(cur) > 0 {
		out = append(out, cur)
	}
	return out
}

func isTable(block []string) bool {
	for _, l := range block {
		if !strings.HasPrefix(strings.TrimSpace(l), "|") {
			return false
		}
	}
	return true
}

// readBody sorts a body's paragraphs into the statement, the rationale, and a test case's preconditions, steps and
// parameters. A table in a requirement's statement stays in it.
func (p *specParser) readBody(it *SpecItem) {
	all := blocks(p.body)
	var statement []string
	for i, b := range all {
		text := strings.Join(b, "\n")
		switch {
		case strings.HasPrefix(text, leadRationale):
			if i != len(all)-1 {
				p.fail(it.Line, it.ID, "the rationale is not the last paragraph. Move it to the end.")
			}
			it.Rationale = strings.TrimSpace(strings.TrimPrefix(text, leadRationale))
		case it.IsTestCase() && strings.HasPrefix(text, leadPreconditions):
			if it.Preconditions != "" {
				p.fail(it.Line, it.ID, "the preconditions are given twice. Make them one paragraph.")
			}
			it.Preconditions = strings.TrimSpace(strings.TrimPrefix(text, leadPreconditions))
		case it.IsTestCase() && strings.HasPrefix(text, leadParameters):
			it.Parameters = p.parameters(it, strings.Join(strings.Fields(strings.TrimPrefix(text, leadParameters)), " "))
		case it.IsTestCase() && isTable(b):
			if it.Steps != nil {
				p.fail(it.Line, it.ID, "the steps are given in two tables. Make them one.")
			}
			it.Steps = p.steps(it, b)
		default:
			statement = append(statement, text)
		}
	}
	it.Statement = strings.Join(statement, "\n\n")
}

// cells splits a table row at its unescaped bars.
func cells(row string) []string {
	row = strings.TrimSpace(row)
	row = strings.TrimSuffix(strings.TrimPrefix(row, "|"), "|")
	var (
		out []string
		cur strings.Builder
	)
	for i := 0; i < len(row); i++ {
		switch {
		case row[i] == '\\' && i+1 < len(row) && row[i+1] == '|':
			cur.WriteByte('|')
			i++
		case row[i] == '|':
			out = append(out, strings.TrimSpace(cur.String()))
			cur.Reset()
		default:
			cur.WriteByte(row[i])
		}
	}
	return append(out, strings.TrimSpace(cur.String()))
}

func (p *specParser) steps(it *SpecItem, rows []string) []SpecStep {
	const want = "a table whose header is `| Action | Expected |`, then `|---|---|`, then a row a step"
	if len(rows) < 2 || !slices.Equal(cells(rows[0]), []string{columnAction, columnExpected}) {
		p.fail(it.Line, it.ID, "the steps are not %s. Write it so.", want)
		return []SpecStep{}
	}
	rule := cells(rows[1])
	if len(rule) != 2 || !tableRule.MatchString(rule[0]) || !tableRule.MatchString(rule[1]) {
		p.fail(it.Line, it.ID, "the table's second line is not `|---|---|`. Write it so.")
	}
	out := []SpecStep{}
	for i, row := range rows[2:] {
		c := cells(row)
		switch {
		case len(c) != 2:
			p.fail(it.Line, it.ID, "step %d has %d cells. Give each step an action and an expected result, two cells.", i+1, len(c))
		case c[0] == "":
			p.fail(it.Line, it.ID, "step %d says nothing to do. Write its action.", i+1)
		default:
			out = append(out, SpecStep{Action: c[0], Expected: c[1]})
		}
	}
	return out
}

func (p *specParser) parameters(it *SpecItem, text string) []requirement.Parameter {
	var out []requirement.Parameter
	for _, part := range strings.Split(text, ";") {
		name, values, ok := strings.Cut(part, "=")
		name = strings.TrimSpace(name)
		if !ok || name == "" {
			p.fail(it.Line, it.ID, "the parameter %q is not `name = value, value`. Write it so, parameters separated by `; `.", strings.TrimSpace(part))
			continue
		}
		param := requirement.Parameter{Name: name}
		for _, v := range strings.Split(values, ",") {
			if v = strings.TrimSpace(v); v == "" {
				p.fail(it.Line, it.ID, "the parameter %s has an empty value. Remove the extra comma.", name)
				continue
			}
			param.Values = append(param.Values, v)
		}
		if len(out) > 0 && len(param.Values) != len(out[0].Values) {
			p.fail(it.Line, it.ID, "the parameter %s has %d values and %s has %d. Give every parameter as many values: each run takes one of each.",
				name, len(param.Values), out[0].Name, len(out[0].Values))
		}
		out = append(out, param)
	}
	return out
}

// check holds an item to what its type needs.
func (p *specParser) check(it *SpecItem) {
	fail := func(format string, args ...any) { p.fail(it.Line, it.ID, format, args...) }
	if it.Type == "" {
		fail("there is no type. Add `type:` with one of %s.", strings.Join(itemTypes, ", "))
		return
	}
	if it.IsTestCase() {
		if len(it.Verifies) == 0 {
			fail("a test case says what it verifies. Add `verifies:` with the IDs of its requirements.")
		}
		if len(it.Steps) == 0 {
			fail("a test case has steps. Add a table of them: `| Action | Expected |`.")
		}
		return
	}
	for _, name := range testCaseFields {
		if p.seen[name] {
			fail("only a test case takes `%s:`. Remove it, or make this a test case.", name)
		}
	}
	if it.Priority == "" {
		fail("there is no priority. Add `priority:` with must, should, could or wont.")
	}
	if it.IsRequirement() && it.Verification == "" {
		fail("a requirement says how it is verified. Add `verification:` with one of %s.", strings.Join(verifications, ", "))
	}
	if it.Statement == "" {
		fail("there is no statement. Write it after the fields and a blank line.")
	}
}

// toolDirs are the folders of spec/ and architecture/, by the key of the tool they hold.
var toolDirs = map[string]string{"ARM": "armature", "EXC": "exciter", "STA": "stator", "ROT": "rotor"}

// GuideLabel is the label Stator's demo gives the guide page a `guide:` names, for the tool of key: the page's own
// name, or `../<tool>/<name>` for another tool's.
func GuideLabel(key, guide string) (string, string, error) {
	name, owner := guide, key
	if rest, ok := strings.CutPrefix(guide, "../"); ok {
		dir, page, found := strings.Cut(rest, "/")
		owner = ""
		for k, d := range toolDirs {
			if d == dir {
				owner = k
			}
		}
		if !found || owner == "" {
			return "", "", fmt.Errorf("guide is %q. Name another tool's page as ../<tool>/<name>, the tool one of armature, exciter, stator or rotor", guide)
		}
		name = page
	}
	if !guideName.MatchString(name) {
		return "", "", fmt.Errorf("guide is %q. Name the page in lower case letters, digits and hyphens, such as boards", guide)
	}
	return "demo-" + strings.ToLower(owner) + "-guide-" + name, owner, nil
}

// readSpec reads the files of one tool's specification in the order of their names, and every mistake in them.
func readSpec(fsys fs.FS, dir string) ([]SpecFile, []error) {
	entries, err := fs.ReadDir(fsys, "spec/"+dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, []error{err}
	}
	var (
		out  []SpecFile
		errs []error
	)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		name := "spec/" + dir + "/" + e.Name()
		raw, err := fs.ReadFile(fsys, name)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		f, err := ParseSpec(name, string(raw))
		if err != nil {
			errs = append(errs, err)
		}
		out = append(out, f)
	}
	return out, errs
}
