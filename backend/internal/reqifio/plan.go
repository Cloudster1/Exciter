package reqifio

import (
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/requirement"
)

// analysis is what a file makes of a project, and what keeps it from starting.
type analysis struct {
	file    *File
	index   *index
	world   *world
	choices map[string]string
	images  map[string]Image

	schema      *schema
	creates     []*createAttr
	linkCreates []*createLink

	preview   Preview
	plans     []*objectPlan
	byRef     map[string]*objectPlan
	relations []*relationPlan
	lossSeen  map[string]bool
	// lossesAt counts the losses by where they are, for the preview's tree.
	lossesAt map[string]int

	// word is the build of a Word document (#209); top is the section it goes below, uuid.Nil the project's top.
	word      *wordBuild
	top       uuid.UUID
	wordFound map[string]wordFound
}

// objectPlan is what one object becomes: a new requirement or section, or a change to one the project has.
type objectPlan struct {
	// Ref is the object's identifier, or spec: and a specification's for the section a specification becomes.
	Ref      string
	Where    string
	Existing *existing
	Kind     requirement.Kind
	// Title is empty to leave an existing one's alone.
	Title     string
	Statement json.RawMessage
	Rationale json.RawMessage
	// Preconditions, Steps and Parameters are what a test case carries (#250), Steps as a table read back later.
	Preconditions json.RawMessage
	Steps         json.RawMessage
	Parameters    *string
	TypeRef       string
	TypeID        *uuid.UUID
	Attributes    map[string]json.RawMessage
	Status        *uuid.UUID
	Owner         *uuid.UUID
	OwnerSet      bool
	Priority      *requirement.Priority
	Labels        []string
	// rich are the rich text values by field or attribute id, read again once the files they show are attachments.
	rich   map[string]*XNode
	Images bool
	// Parent is the Ref of the plan above, empty for the top of the project.
	Parent string
	// KeepPlace leaves an existing requirement where it is: at the top of the file, or in no specification.
	KeepPlace bool
	// Specification is the identifier of the specification a section stands for.
	Specification string
	// MatchedBy is how a Word import found Existing.
	MatchedBy WordMatch
}

// relationPlan is a link a relation asks for.
type relationPlan struct {
	Where          string
	Type           string
	Source, Target string
}

// analyze reads a file against the project with the choices made.
func analyze(f *File, w *world, choices map[string]string) *analysis {
	return analyzeWith(f, w, choices, nil, uuid.Nil)
}

// analyzeWith reads a file, or what a Word document was built into, against the project.
func analyzeWith(f *File, w *world, choices map[string]string, word *wordBuild, top uuid.UUID) *analysis {
	a := &analysis{file: f, index: indexOf(f), world: w, choices: choices, images: map[string]Image{}, byRef: map[string]*objectPlan{},
		lossSeen: map[string]bool{}, lossesAt: map[string]int{}, word: word, top: top}
	for _, img := range f.Images {
		a.images[img.Path] = img
	}
	a.preview = Preview{Title: f.Title, SourceTool: f.SourceTool, Specifications: []string{}, Objects: len(f.Objects),
		Relations: len(f.Relations), Images: len(f.Images), Schema: []SchemaItem{}, Losses: []Loss{}, Problems: []Problem{},
		Tree: []PreviewNode{}, Styles: []StyleUse{}}
	for _, s := range f.Specifications {
		if !isGlossary(s) {
			a.preview.Specifications = append(a.preview.Specifications, nameOf(s.Identity))
		}
	}
	a.resolveSchema()
	if word != nil {
		a.readWord()
	}
	a.planObjects()
	a.planRelations()
	for _, item := range a.schema.items {
		a.preview.Schema = append(a.preview.Schema, *item)
	}
	for _, p := range a.plans {
		if p.Existing != nil {
			a.preview.Updates++
		} else {
			a.preview.New++
		}
		if p.Kind == requirement.KindSection {
			a.preview.Sections++
		}
	}
	a.preview.Tree = a.tree()
	return a
}

func (a *analysis) lose(kind LossKind, where, format string, args ...any) {
	message := fmt.Sprintf(format, args...)
	if a.lossSeen[where+"\x00"+message] {
		return
	}
	a.lossSeen[where+"\x00"+message] = true
	a.lossesAt[where]++
	a.preview.LostCount++
	if len(a.preview.Losses) < MaxLosses {
		a.preview.Losses = append(a.preview.Losses, Loss{Kind: kind, Where: where, Message: message})
	}
}

// placer stands in for the attachments a file's images become, so the analysis reads documents as they will be.
func (a *analysis) placer(ref string) (Placed, bool) {
	img, ok := a.images[ref]
	if !ok {
		return Placed{}, false
	}
	return Placed{ID: uuid.Nil.String(), Image: imageTypes[img.ContentType], FileName: path.Base(ref)}, true
}

// textOf is a value as plain text.
func textOf(v *Value) string {
	if v.XHTML != nil {
		return strings.TrimSpace(whitespace.ReplaceAllString(Words(v.XHTML), " "))
	}
	return strings.TrimSpace(v.Text)
}

// fieldValue is the object's value of the field, nil when it has none.
func (a *analysis) fieldValue(o *Object, field string) *Value {
	for i := range o.Values {
		if d := a.index.defs[o.Values[i].Def]; d != nil && fieldOf(d) == field {
			return &o.Values[i]
		}
	}
	return nil
}

// isSection says an object is a heading: of an export's section type, or with a chapter name and no text.
func (a *analysis) isSection(o *Object) bool {
	if o.Type == sectionType {
		return true
	}
	chapter, text := a.fieldValue(o, fieldChapter), a.fieldValue(o, fieldStatement)
	return chapter != nil && textOf(chapter) != "" && (text == nil || textOf(text) == "")
}

// whereOf names an object as a person finds it in the file.
func (a *analysis) whereOf(o *Object) string {
	for _, field := range []string{fieldTitle, fieldChapter} {
		if v := a.fieldValue(o, field); v != nil && textOf(v) != "" {
			return truncate(textOf(v), whereLength)
		}
	}
	return nameOf(o.Identity)
}

// whereLength bounds how much of a title names an object in a loss or a problem.
const whereLength = 80

// isGlossary says a specification is the glossary an export wrote, which an import leaves out (#210).
func isGlossary(spec *Specification) bool { return spec.Type == glossarySpecType }

// isTerm says an object is a term of an export's glossary.
func isTerm(o *Object) bool { return o.Type == glossaryTermType }

func (a *analysis) planObjects() {
	placed := map[string]bool{}
	specs, glossary := 0, false
	for _, spec := range a.file.Specifications {
		if isGlossary(spec) {
			glossary = true
			continue
		}
		specs++
		a.walk(spec.Children, a.container(spec), placed)
	}
	loose := 0
	for _, o := range a.file.Objects {
		if isTerm(o) {
			glossary = true
			continue
		}
		if !placed[o.Identifier] {
			loose++
			a.planObject(o, "")
		}
	}
	if glossary {
		a.lose(LossPlace, "", "The glossary of the file is left out; terms are not imported.")
	}
	if loose > 0 && specs > 0 {
		a.lose(LossPlace, "", "%d objects are in no specification, so they go at the top of the project.", loose)
	}
}

func (a *analysis) walk(nodes []*Node, parent string, placed map[string]bool) {
	for _, n := range nodes {
		o := a.index.objects[n.Object]
		if o == nil {
			a.lose(LossPlace, "", "The hierarchy names the object %s, which the file does not hold.", n.Object)
			continue
		}
		if isTerm(o) {
			continue
		}
		if placed[o.Identifier] {
			a.lose(LossPlace, a.whereOf(o), "It appears twice in the hierarchy, and stays at its first place.")
			a.walk(n.Children, o.Identifier, placed)
			continue
		}
		placed[o.Identifier] = true
		a.planObject(o, parent)
		a.walk(n.Children, o.Identifier, placed)
	}
}

// container is the plan a specification's objects go below: none for the project itself, which a specification
// an export wrote is, else the section the specification is or becomes.
func (a *analysis) container(spec *Specification) string {
	if strings.HasPrefix(spec.Identifier, prefixProject) {
		return ""
	}
	ref := "spec:" + spec.Identifier
	p := &objectPlan{Ref: ref, Kind: requirement.KindSection, Specification: spec.Identifier, Title: truncate(nameOf(spec.Identity), requirement.MaxTitleLength)}
	if spec.Name == "" {
		p.Title = "Imported specification"
	}
	p.Where = p.Title
	if tie, ok := a.world.tied("specification", spec.Identifier, ""); ok {
		if tie.Target == a.world.project.ID {
			return ""
		}
		if e := a.world.existing[tie.Target]; e != nil && e.Kind == requirement.KindSection {
			p.Existing, p.KeepPlace = e, true
		}
	}
	a.plans = append(a.plans, p)
	a.byRef[ref] = p
	return ref
}

func (a *analysis) planObject(o *Object, parent string) {
	where := a.whereOf(o)
	p := &objectPlan{Ref: o.Identifier, Parent: parent, Where: where, Kind: requirement.KindRequirement, Attributes: map[string]json.RawMessage{}, rich: map[string]*XNode{}}
	if a.isSection(o) {
		p.Kind = requirement.KindSection
	}
	var found *existing
	if a.word != nil {
		if f, ok := a.wordFound[o.Identifier]; ok {
			found, p.MatchedBy = a.world.existing[f.target], f.by
		}
		for _, l := range a.word.parts[o.Identifier].losses {
			a.lose(LossKind(l.Kind), where, "%s", l.Message)
		}
	} else if tie, ok := a.world.tied("object", o.Identifier, prefixObject); ok {
		found = a.world.existing[tie.Target]
	}
	if e := found; e != nil {
		p.Existing = e
		if e.Kind != p.Kind && e.Kind == requirement.KindSection {
			a.lose(LossValue, where, "It is a section in the project, so its type and attributes are left out.")
		}
		p.Kind = e.Kind
	}
	// A Word document's parts follow its order wherever an earlier import left them.
	p.KeepPlace = p.Existing != nil && parent == "" && a.word == nil
	if p.Kind == requirement.KindRequirement {
		if t := a.schema.types[o.Type]; t != nil {
			p.TypeRef, p.TypeID = o.Type, t.id
		}
	}
	var name, chapter string
	for i := range o.Values {
		a.planValue(p, o, &o.Values[i], &name, &chapter)
	}
	a.planTitle(p, o, name, chapter)
	a.settleTestCase(p, where)
	if p.Kind == requirement.KindRequirement && p.Existing == nil {
		a.checkRequired(p)
	}
	for _, x := range p.rich {
		x.walk(func(n *XNode) bool {
			if n.Name == "object" {
				if chosen, ok := chooseObject(n); ok && a.images[strings.TrimSpace(chosen.Attrs["data"])].Path != "" {
					p.Images = true
				}
				return false
			}
			return true
		})
	}
	a.plans = append(a.plans, p)
	a.byRef[p.Ref] = p
}

// planTitle settles the title: a requirement's name first, a section's chapter first, then the object's name,
// then the first words of its statement. An existing one's stays when the file names none.
func (a *analysis) planTitle(p *objectPlan, o *Object, name, chapter string) {
	candidates := []string{name, chapter, o.Name}
	if p.Kind == requirement.KindSection {
		candidates = []string{chapter, name, o.Name}
	}
	for _, c := range candidates {
		if c = strings.TrimSpace(c); c != "" {
			p.Title = c
			break
		}
	}
	if p.Title == "" && p.Existing == nil {
		if x := p.rich[fieldStatement]; x != nil {
			p.Title, _, _ = strings.Cut(strings.TrimSpace(Words(x)), "\n")
		}
		if p.Title = strings.TrimSpace(p.Title); p.Title == "" {
			p.Title = o.Identifier
		}
	}
	if utf8.RuneCountInString(p.Title) > requirement.MaxTitleLength {
		p.Title = truncate(p.Title, requirement.MaxTitleLength)
		a.lose(LossValue, p.Where, "Its title is longer than %d characters and was cut there.", requirement.MaxTitleLength)
	}
}

func (a *analysis) planRelations() {
	for _, r := range a.file.Relations {
		where := nameOf(r.Identity)
		source, target := a.endOf(r.Source), a.endOf(r.Target)
		switch {
		case source == "" || target == "":
			a.lose(LossRelation, where, "The relation names an object the file does not hold and the project has no requirement for, so it is left out.")
		case source == "section" || target == "section":
			a.lose(LossRelation, where, "The relation joins a section, and links join requirements, so it is left out.")
		case a.schema.links[r.Type] == nil:
			a.lose(LossRelation, where, "The relation has no type the file defines, so it is left out.")
		default:
			a.relations = append(a.relations, &relationPlan{Where: where, Type: r.Type, Source: r.Source, Target: r.Target})
		}
	}
}

// endOf says what a relation's end is: a requirement, a section, or nothing the import knows.
func (a *analysis) endOf(identifier string) string {
	if p := a.byRef[identifier]; p != nil {
		return string(p.Kind)
	}
	if tie, ok := a.world.tied("object", identifier, prefixObject); ok {
		if e := a.world.existing[tie.Target]; e != nil {
			return string(e.Kind)
		}
	}
	return ""
}
