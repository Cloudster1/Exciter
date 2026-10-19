package reqifio

import (
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/attribute"
	"github.com/cloudster1/exciter/backend/internal/trace"
)

// typeTarget is what a SPEC-OBJECT-TYPE becomes: a type of the project, one to make, or none.
type typeTarget struct {
	item   *SchemaItem
	id     *uuid.UUID
	create string
	// testCases says the file's type carries what test cases do (#250), so a type made for it is one of test cases.
	testCases bool
	// risks says the file's type carries a rating (#259), so a type made for it is one of risks.
	risks bool
	// none is the type an export writes for requirements without one.
	none bool
}

// attrTarget is what an ATTRIBUTE-DEFINITION becomes: a field, an attribute of the project, one to make, or
// nothing.
type attrTarget struct {
	item   *SchemaItem
	field  string
	def    *attribute.Def
	create *createAttr
	skip   bool
	// lost says its values have nowhere to go, as an attribute of a heading.
	lost bool
	// missing are option names the attribute lacks, added when the import runs.
	missing []string
}

// kind is the kind of attribute its values take.
func (a *attrTarget) kind() attribute.Kind {
	if a.def != nil {
		return a.def.Kind
	}
	if a.create != nil {
		return a.create.kind
	}
	return ""
}

// createAttr is an attribute an import makes, shared by the definitions of the same name on several types.
type createAttr struct {
	name    string
	kind    attribute.Kind
	options []string
	// types are the spec types of the file it shows on.
	types []string
	made  *attribute.Def
}

// linkTarget is what a SPEC-RELATION-TYPE becomes: a link type the project uses, or one to make.
type linkTarget struct {
	item   *SchemaItem
	id     *uuid.UUID
	create *createLink
}

type createLink struct {
	key, name string
}

// schema is how a file's types, attributes and link types map onto the project.
type schema struct {
	types map[string]*typeTarget
	attrs map[string]*attrTarget
	links map[string]*linkTarget
	items []*SchemaItem
}

// compatible lists the kinds of attribute each datatype's values fit, the one an import makes first.
var compatible = map[DataKind][]attribute.Kind{
	KindString:  {attribute.Text, attribute.RichText, attribute.URL, attribute.User},
	KindXHTML:   {attribute.RichText, attribute.Text},
	KindInteger: {attribute.Number},
	KindReal:    {attribute.Number},
	KindBoolean: {attribute.Boolean},
	KindDate:    {attribute.Date},
	KindEnum:    {attribute.Select, attribute.MultiSelect},
}

func fits(d *AttrDef, kind attribute.Kind) bool {
	return slices.Contains(fitting(d), kind)
}

// fitting are the kinds of attribute a definition's values fit; several values fit only a multi-select.
func fitting(d *AttrDef) []attribute.Kind {
	if d.Kind == KindEnum && d.Multi {
		return []attribute.Kind{attribute.MultiSelect}
	}
	return compatible[d.Kind]
}

// resolveSchema maps the types, attributes and link types the file's objects and relations use, by the choices
// made, then the identifiers of earlier exchanges, then their names, and makes the rest.
func (a *analysis) resolveSchema() {
	a.schema = &schema{types: map[string]*typeTarget{}, attrs: map[string]*attrTarget{}, links: map[string]*linkTarget{}}
	used := map[string]int{}
	sectionsOf := map[string]int{}
	for _, o := range a.file.Objects {
		if isTerm(o) {
			continue
		}
		if a.isSection(o) {
			sectionsOf[o.Type]++
		} else {
			used[o.Type]++
		}
	}
	for _, t := range a.file.Types {
		if t.Kind != TypeObject || (used[t.Identifier] == 0 && sectionsOf[t.Identifier] == 0) {
			continue
		}
		if used[t.Identifier] > 0 {
			a.resolveType(t, used[t.Identifier])
		} else {
			a.appendItem(SchemaItem{Kind: SchemaType, Identifier: t.Identifier, Name: nameOf(t.Identity), By: BySection, Count: sectionsOf[t.Identifier]})
		}
		for _, d := range t.Attributes {
			a.resolveAttribute(t, d, used[t.Identifier] == 0)
		}
	}
	relations := map[string]int{}
	for _, r := range a.file.Relations {
		relations[r.Type]++
	}
	for _, t := range a.file.Types {
		if t.Kind == TypeRelation && relations[t.Identifier] > 0 {
			a.resolveLinkType(t, relations[t.Identifier])
		}
	}
}

func nameOf(id Identity) string {
	if id.Name != "" {
		return id.Name
	}
	return id.Identifier
}

func (a *analysis) problem(where, format string, args ...any) {
	a.preview.Problems = append(a.preview.Problems, Problem{Where: where, Message: fmt.Sprintf(format, args...)})
}

// chosen reads a choice made for an identifier: an id, create, skip or nothing.
func (a *analysis) chosen(identifier string) (id *uuid.UUID, choice string) {
	c, ok := a.choices[identifier]
	if !ok {
		return nil, ""
	}
	if c == ChoiceCreate || c == ChoiceSkip {
		return nil, c
	}
	parsed, err := uuid.Parse(c)
	if err != nil {
		return nil, "invalid"
	}
	return &parsed, "id"
}

func (a *analysis) resolveType(t *SpecType, count int) {
	item := SchemaItem{Kind: SchemaType, Identifier: t.Identifier, Name: nameOf(t.Identity), Count: count, By: ByNone}
	target := &typeTarget{}
	id, choice := a.chosen(t.Identifier)
	named, byName := a.world.typeNamed(item.Name)
	switch {
	case choice == "id" && a.world.types[*id].ID != uuid.Nil:
		target.id, item.By = id, ByChoice
	case choice == "id" || choice == "invalid" || choice == ChoiceSkip:
		a.problem(item.Name, "The type chosen for %s is not one of the project's. Pick one of its types, or have the import make one.", item.Name)
	case choice == ChoiceCreate:
		target.create, item.By = a.uniqueTypeName(item.Name), ByCreate
	case t.Identifier == untypedType:
		target.none, item.By = true, ByIdentifier
	default:
		if tie, ok := a.world.tied("type", t.Identifier, prefixType); ok && a.world.types[tie.Target].ID != uuid.Nil {
			target.id, item.By = &tie.Target, ByIdentifier
		} else if byName {
			target.id, item.By = &named.ID, ByName
		} else {
			target.create, item.By = a.uniqueTypeName(item.Name), ByCreate
		}
	}
	if target.id != nil {
		item.Means, item.MeansName = target.id, a.world.types[*target.id].Name
	}
	for _, d := range t.Attributes {
		if f := fieldOf(d); f == fieldSteps || f == fieldPreconditions || f == fieldParameters {
			target.testCases = true
		} else if f == fieldRisk {
			target.risks = true
		}
	}
	target.item = a.appendItem(item)
	a.schema.types[t.Identifier] = target
}

// uniqueTypeName is the name a type is made with: the file's, cut to fit, the same for the same name.
func (a *analysis) uniqueTypeName(name string) string {
	name = strings.TrimSpace(truncate(name, attribute.MaxNameLength))
	if _, taken := a.world.typeNamed(name); taken {
		name = strings.TrimSpace(truncate(name, attribute.MaxNameLength-len(" (ReqIF)"))) + " (ReqIF)"
	}
	return name
}

func (a *analysis) appendItem(item SchemaItem) *SchemaItem {
	a.schema.items = append(a.schema.items, &item)
	return &item
}

func (a *analysis) resolveAttribute(t *SpecType, d *AttrDef, ofHeadings bool) {
	item := SchemaItem{Kind: SchemaAttribute, Identifier: d.Identifier, Name: nameOf(d.Identity), Count: a.valueCount(d.Identifier), By: ByNone}
	target := &attrTarget{}
	defer func() {
		target.item = a.appendItem(item)
		a.schema.attrs[d.Identifier] = target
	}()
	if field := fieldOf(d); field != "" {
		target.field, item.By, item.Field = field, ByField, field
		return
	}
	item.Fits = fitting(d)
	if ofHeadings {
		target.lost, item.By = true, BySection
		if item.Count > 0 {
			a.lose(LossValue, nameOf(t.Identity), "Headings keep no attributes, so the values of %s are left out.", item.Name)
		}
		return
	}
	id, choice := a.chosen(d.Identifier)
	switch choice {
	case ChoiceSkip:
		target.skip, item.By = true, BySkip
		return
	case ChoiceCreate:
		a.planCreate(t, d, target, &item)
		return
	case "id":
		if def := a.world.def(*id); def != nil && !def.Retired && fits(d, def.Kind) {
			a.matched(target, &item, def, ByChoice)
			return
		}
		a.problem(item.Name, "The attribute chosen for %s cannot hold its values. Pick an attribute of a fitting kind, have the import make one, or leave it out.", item.Name)
		return
	case "invalid":
		a.problem(item.Name, "The choice for %s names nothing of the project. Pick one of its attributes, have the import make one, or leave it out.", item.Name)
		return
	}
	if tie, ok := a.world.tied("attribute", d.Identifier, ""); ok {
		if def := a.world.def(tie.Target); def != nil && !def.Retired && fits(d, def.Kind) {
			a.matched(target, &item, def, ByIdentifier)
			return
		}
	}
	if id, ok := attrOf(d.Identifier); ok {
		if def := a.world.def(id); def != nil && !def.Retired && fits(d, def.Kind) {
			a.matched(target, &item, def, ByIdentifier)
			return
		}
	}
	if def := a.world.defNamed(item.Name); def != nil {
		if fits(d, def.Kind) {
			a.matched(target, &item, def, ByName)
			return
		}
		a.problem(item.Name, "The file's %s holds %s values, and the project's %s is a %s attribute. Map it to another attribute, have the import make one, or leave it out.",
			item.Name, d.Kind, def.Name, def.Kind)
		return
	}
	a.planCreate(t, d, target, &item)
}

func (a *analysis) matched(target *attrTarget, item *SchemaItem, def *attribute.Def, by MatchBy) {
	target.def = def
	item.Means, item.MeansName, item.By, item.Becomes = &def.ID, def.Name, by, def.Kind
}

// planCreate shares one attribute to make among the definitions of a name whose values fit the same kind.
func (a *analysis) planCreate(t *SpecType, d *AttrDef, target *attrTarget, item *SchemaItem) {
	kind := a.kindToMake(d)
	name := strings.TrimSpace(truncate(item.Name, attribute.MaxNameLength))
	for _, c := range a.creates {
		if strings.EqualFold(c.name, name) && c.kind == kind {
			c.types = appendNew(c.types, t.Identifier)
			c.options = a.withOptions(c.options, d)
			target.create, item.By, item.Becomes = c, ByCreate, kind
			return
		}
		if strings.EqualFold(c.name, name) {
			name = strings.TrimSpace(truncate(name, attribute.MaxNameLength-len(nameOf(t.Identity))-len(" ()"))) + " (" + nameOf(t.Identity) + ")"
		}
	}
	if a.world.defNamed(name) != nil {
		a.problem(item.Name, "The project already has an attribute named %s. Map the file's %s to it or to another attribute, or leave it out.", name, item.Name)
		return
	}
	c := &createAttr{name: name, kind: kind, types: []string{t.Identifier}, options: a.withOptions(nil, d)}
	if len(c.options) > attribute.MaxOptions {
		a.problem(item.Name, "%s has %d values to choose from, more than the %d an attribute holds. Map it to text, or leave it out.", item.Name, len(c.options), attribute.MaxOptions)
	}
	a.creates = append(a.creates, c)
	target.create, item.By, item.Becomes = c, ByCreate, kind
}

func appendNew(list []string, s string) []string {
	if slices.Contains(list, s) {
		return list
	}
	return append(list, s)
}

// kindToMake is the kind an attribute is made as; text past an attribute's length becomes rich text.
func (a *analysis) kindToMake(d *AttrDef) attribute.Kind {
	if d.Kind == KindEnum && d.Multi {
		return attribute.MultiSelect
	}
	if d.Kind == KindString && d.DataType == dataUser {
		return attribute.User
	}
	if d.Kind == KindString && d.DataType == dataURL {
		return attribute.URL
	}
	if d.Kind == KindString {
		if dt := a.index.dataTypes[d.DataType]; dt != nil && dt.MaxLength > attribute.MaxTextLength && a.longest(d.Identifier) > attribute.MaxTextLength {
			return attribute.RichText
		}
	}
	return compatible[d.Kind][0]
}

func (a *analysis) longest(def string) int {
	most := 0
	for _, o := range a.file.Objects {
		if v := o.value(def); v != nil {
			most = max(most, len([]rune(strings.TrimSpace(v.Text))))
		}
	}
	return most
}

func (a *analysis) withOptions(options []string, d *AttrDef) []string {
	if dt := a.index.dataTypes[d.DataType]; dt != nil {
		for _, v := range dt.Values {
			name := strings.TrimSpace(truncate(nameOf(v.Identity), attribute.MaxNameLength))
			if !slices.ContainsFunc(options, func(o string) bool { return strings.EqualFold(o, name) }) {
				options = append(options, name)
			}
		}
	}
	return options
}

func (a *analysis) valueCount(def string) int {
	n := 0
	for _, o := range a.file.Objects {
		if o.value(def) != nil {
			n++
		}
	}
	return n
}

func (a *analysis) resolveLinkType(t *SpecType, count int) {
	item := SchemaItem{Kind: SchemaLinkType, Identifier: t.Identifier, Name: nameOf(t.Identity), Count: count, By: ByNone}
	target := &linkTarget{}
	id, choice := a.chosen(t.Identifier)
	switch {
	case choice == "id" && a.world.linkTypes[*id].ID != uuid.Nil:
		target.id, item.By = id, ByChoice
	case choice == "id" || choice == "invalid" || choice == ChoiceSkip:
		a.problem(item.Name, "The link type chosen for %s is not one the project uses. Pick one of its link types, or have the import make one.", item.Name)
	case choice == ChoiceCreate:
		target.create, item.By = a.linkToMake(item.Name), ByCreate
	default:
		if tie, ok := a.world.tied("relation_type", t.Identifier, prefixLinkType); ok && a.world.linkTypes[tie.Target].ID != uuid.Nil {
			target.id, item.By = &tie.Target, ByIdentifier
		} else if l, ok := a.linkNamed(item.Name); ok {
			target.id, item.By = &l.ID, ByName
		} else {
			target.create, item.By = a.linkToMake(item.Name), ByCreate
		}
	}
	if target.id != nil {
		item.Means, item.MeansName = target.id, a.world.linkTypes[*target.id].Name
	}
	target.item = a.appendItem(item)
	a.schema.links[t.Identifier] = target
}

// linkNamed is the link type whose key or name the file's relation type has.
func (a *analysis) linkNamed(name string) (linkType, bool) {
	key := keyOf(name, func(string) bool { return false })
	for _, l := range a.world.linkTypes {
		if l.Key == key || strings.EqualFold(l.Name, name) {
			return l, true
		}
	}
	return linkType{}, false
}

func (a *analysis) linkToMake(name string) *createLink {
	for _, c := range a.linkCreates {
		if strings.EqualFold(c.name, truncate(name, trace.MaxNameLength)) {
			return c
		}
	}
	taken := func(key string) bool {
		for _, l := range a.world.linkTypes {
			if l.Key == key {
				return true
			}
		}
		return slices.ContainsFunc(a.linkCreates, func(c *createLink) bool { return c.key == key })
	}
	c := &createLink{key: keyOf(name, taken), name: strings.TrimSpace(truncate(name, trace.MaxNameLength))}
	a.linkCreates = append(a.linkCreates, c)
	return c
}
