package reqifio

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/attribute"
	"github.com/cloudster1/exciter/backend/internal/requirement"
)

// The datatypes every export defines; an enumeration of options is one more per attribute.
const (
	dataString  = "exciter-dt-string"
	dataXHTML   = "exciter-dt-xhtml"
	dataNumber  = "exciter-dt-number"
	dataBoolean = "exciter-dt-boolean"
	dataDate    = "exciter-dt-date"
	// dataUser and dataURL are strings whose datatype tells an import which kind of attribute to make of them.
	dataUser = "exciter-dt-user"
	dataURL  = "exciter-dt-url"
	// exportStringLength bounds a string an export writes, past any title, text attribute or label list.
	exportStringLength = 1 << 16
	// numberAccuracy is the decimal digits a stored number keeps.
	numberAccuracy = 15
	// dateLayout writes a day as xsd:dateTime at midnight UTC, which an import reads back as the same day.
	dateLayout = "2006-01-02T00:00:00.000Z"
)

// priorityNames are what an export calls each priority.
var priorityNames = map[requirement.Priority]string{
	requirement.PriorityMust: "Must", requirement.PriorityShould: "Should", requirement.PriorityCould: "Could", requirement.PriorityWont: "Won't",
}

// builder turns a snapshot of a project into a file.
type builder struct {
	snap    *snapshot
	file    *File
	fileRef FileRef
	now     time.Time
	// typeIDs are the spec types written, by the type id they are for; uuid.Nil is the one for no type.
	typeIDs map[uuid.UUID]string
	// defs are a spec type's attribute definitions by field or attribute id.
	defs map[string]map[string]*AttrDef
}

func (snap *snapshot) build(fileRef FileRef) *File {
	now := snap.now
	title := snap.project.Name
	if snap.root != nil {
		title = snap.root.Title
	}
	b := &builder{snap: snap, fileRef: fileRef, now: now, typeIDs: map[uuid.UUID]string{}, defs: map[string]map[string]*AttrDef{}}
	b.file = &File{Identifier: prefixExport + snap.exportID, Title: title, SourceTool: "Exciter", ToolID: snap.tool, Created: now,
		Comment: "Exported from the project " + snap.project.Key + "."}
	b.dataTypes()
	b.specTypes()
	for _, n := range snap.nodes {
		b.object(n)
	}
	b.relations()
	b.specification()
	b.glossary()
	return b.file
}

func (b *builder) id(identifier, name string) Identity {
	return Identity{Identifier: identifier, Name: name, Changed: b.now}
}

func (b *builder) dataTypes() {
	f := b.file
	f.DataTypes = append(f.DataTypes,
		&DataType{Identity: b.id(dataString, "Text"), Kind: KindString, MaxLength: exportStringLength},
		&DataType{Identity: b.id(dataXHTML, "Rich text"), Kind: KindXHTML},
		&DataType{Identity: b.id(dataNumber, "Number"), Kind: KindReal, Accuracy: numberAccuracy, Min: "-1.7976931348623157E308", Max: "1.7976931348623157E308"},
		&DataType{Identity: b.id(dataBoolean, "Yes or no"), Kind: KindBoolean},
		&DataType{Identity: b.id(dataDate, "Date"), Kind: KindDate},
		&DataType{Identity: b.id(dataUser, "Person"), Kind: KindString, MaxLength: exportStringLength},
		&DataType{Identity: b.id(dataURL, "Web address"), Kind: KindString, MaxLength: exportStringLength})
	status := &DataType{Identity: b.id(statusEnum, "Status"), Kind: KindEnum}
	for i, s := range b.snap.statuses {
		status.Values = append(status.Values, EnumValue{Identity: b.id(prefixStatus+s.ID.String(), s.Name), Key: i})
	}
	priority := &DataType{Identity: b.id(priorityEnum, "Priority"), Kind: KindEnum}
	for i, p := range requirement.Priorities {
		priority.Values = append(priority.Values, EnumValue{Identity: b.id(prefixPriority+string(p), priorityNames[p]), Key: i})
	}
	f.DataTypes = append(f.DataTypes, status, priority)
	for _, d := range b.snap.defs {
		if d.Kind != attribute.Select && d.Kind != attribute.MultiSelect {
			continue
		}
		enum := &DataType{Identity: b.id(prefixDataEnum+d.ID.String(), d.Name), Kind: KindEnum}
		for i, o := range d.Options {
			enum.Values = append(enum.Values, EnumValue{Identity: b.id(b.snap.identifier("option", o.ID, nil, prefixOption+o.ID.String()), o.Name), Key: i})
		}
		f.DataTypes = append(f.DataTypes, enum)
	}
}

// dataOf is the datatype and kind an attribute of the project is written with.
func dataOf(d attribute.Def) (string, DataKind) {
	switch d.Kind {
	case attribute.RichText:
		return dataXHTML, KindXHTML
	case attribute.Number:
		return dataNumber, KindReal
	case attribute.Boolean:
		return dataBoolean, KindBoolean
	case attribute.Date:
		return dataDate, KindDate
	case attribute.Select, attribute.MultiSelect:
		return prefixDataEnum + d.ID.String(), KindEnum
	case attribute.User:
		return dataUser, KindString
	case attribute.URL:
		return dataURL, KindString
	}
	return dataString, KindString
}

// requirementFields are the fields a requirement's type writes, in order, with their datatypes.
var requirementFields = []struct {
	field, dataType string
	kind            DataKind
}{
	{fieldKey, dataString, KindString}, {fieldTitle, dataString, KindString}, {fieldStatement, dataXHTML, KindXHTML},
	{fieldRationale, dataXHTML, KindXHTML}, {fieldStatus, statusEnum, KindEnum}, {fieldPriority, priorityEnum, KindEnum},
	{fieldOwner, dataString, KindString}, {fieldLabels, dataString, KindString},
}

// testCaseFields are what the type of test cases writes besides (#250): the steps as a table of action and result.
var testCaseFields = []struct {
	field, dataType string
	kind            DataKind
}{
	{fieldPreconditions, dataXHTML, KindXHTML}, {fieldSteps, dataXHTML, KindXHTML}, {fieldParameters, dataString, KindString},
}

func (b *builder) specTypes() {
	used := map[uuid.UUID]bool{}
	sections := false
	for _, n := range b.snap.nodes {
		switch {
		case n.Kind == requirement.KindSection:
			sections = true
		case n.TypeID == nil:
			used[uuid.Nil] = true
		default:
			used[*n.TypeID] = true
		}
	}
	if sections {
		t, defs := &SpecType{Identity: b.id(sectionType, "Section"), Kind: TypeObject}, map[string]*AttrDef{}
		for _, field := range []string{fieldKey, fieldChapter, fieldStatement} {
			kind, dataType := KindString, dataString
			if field == fieldStatement {
				kind, dataType = KindXHTML, dataXHTML
			}
			defs[field] = &AttrDef{Identity: b.id(fieldIdentifier("section", field), fieldNames[field]), Kind: kind, DataType: dataType}
			t.Attributes = append(t.Attributes, defs[field])
		}
		b.addType(t, defs)
	}
	for _, typeID := range b.orderedTypes(used) {
		identifier, name, scope := untypedType, "Requirement", "none"
		if typeID != uuid.Nil {
			identifier = b.snap.identifier("type", typeID, nil, prefixType+typeID.String())
			name, scope = b.snap.types[typeID].Name, typeID.String()
		}
		t, defs := &SpecType{Identity: b.id(identifier, name), Kind: TypeObject}, map[string]*AttrDef{}
		if at, ok := b.snap.changed[typeID]; ok {
			t.Changed = at
		}
		b.typeIDs[typeID] = identifier
		fields := requirementFields
		if b.snap.testTypes[typeID] {
			fields = append(fields[:len(fields):len(fields)], testCaseFields...)
		}
		for _, f := range fields {
			defs[f.field] = &AttrDef{Identity: b.id(fieldIdentifier(scope, f.field), fieldNames[f.field]), Kind: f.kind, DataType: f.dataType}
			t.Attributes = append(t.Attributes, defs[f.field])
		}
		var forType *uuid.UUID
		if typeID != uuid.Nil {
			forType = &typeID
		}
		for _, d := range b.snap.defs {
			if d.Retired || !d.Shows(forType) {
				continue
			}
			dataType, kind := dataOf(d)
			identifier := attrIdentifier(d.ID, typeID)
			if forType != nil {
				identifier = b.snap.identifier("attribute", d.ID, forType, identifier)
			}
			defs[attrKey(d.ID)] = &AttrDef{Identity: b.id(identifier, d.Name), Kind: kind, DataType: dataType, Multi: d.Kind == attribute.MultiSelect}
			t.Attributes = append(t.Attributes, defs[attrKey(d.ID)])
		}
		b.addType(t, defs)
	}
	b.file.Types = append(b.file.Types, &SpecType{Identity: b.id(specificationType, "Specification"), Kind: TypeSpecification})
	kinds := make([]uuid.UUID, 0, len(b.snap.kinds))
	for _, l := range b.snap.links {
		if !containsID(kinds, l.Type) {
			kinds = append(kinds, l.Type)
		}
	}
	for _, id := range kinds {
		lt := b.snap.kinds[id]
		b.file.Types = append(b.file.Types, &SpecType{Identity: b.id(b.snap.identifier("relation_type", id, nil, prefixLinkType+id.String()), lt.Name), Kind: TypeRelation})
	}
}

func containsID(list []uuid.UUID, id uuid.UUID) bool {
	for _, have := range list {
		if have == id {
			return true
		}
	}
	return false
}

// orderedTypes are the types used, in the order the tree first meets them.
func (b *builder) orderedTypes(used map[uuid.UUID]bool) []uuid.UUID {
	var out []uuid.UUID
	for _, n := range b.snap.nodes {
		id := uuid.Nil
		if n.TypeID != nil {
			id = *n.TypeID
		}
		if n.Kind == requirement.KindRequirement && used[id] && !containsID(out, id) {
			out = append(out, id)
		}
	}
	return out
}

// attrKey finds an attribute of the project among a spec type's definitions, apart from the fields.
func attrKey(id uuid.UUID) string { return "attr:" + id.String() }

func (b *builder) addType(t *SpecType, defs map[string]*AttrDef) {
	b.file.Types = append(b.file.Types, t)
	b.defs[t.Identifier] = defs
}

func (b *builder) objectID(id uuid.UUID) string {
	return b.snap.identifier("object", id, nil, prefixObject+id.String())
}

func (b *builder) object(n requirement.TreeNode) {
	c := b.snap.contents[n.ID]
	o := &Object{Identity: Identity{Identifier: b.objectID(n.ID), Changed: c.Updated}}
	typeID := uuid.Nil
	if n.TypeID != nil {
		typeID = *n.TypeID
	}
	o.Type = b.typeIDs[typeID]
	if n.Kind == requirement.KindSection {
		o.Type = sectionType
	}
	defs := b.defs[o.Type]
	put := func(field string, v Value) {
		if d := defs[field]; d != nil {
			v.Def = d.Identifier
			o.Values = append(o.Values, v)
		}
	}
	put(fieldKey, Value{Text: n.Key})
	if n.Kind == requirement.KindSection {
		put(fieldChapter, Value{Text: n.Title})
	} else {
		put(fieldTitle, Value{Text: n.Title})
	}
	put(fieldStatement, Value{XHTML: FromDocument(c.Statement, b.fileRef)})
	if n.Kind == requirement.KindRequirement {
		if len(c.Rationale) > 0 && string(c.Rationale) != "null" {
			put(fieldRationale, Value{XHTML: FromDocument(c.Rationale, b.fileRef)})
		}
		if c.Status != nil {
			put(fieldStatus, Value{Enum: []string{prefixStatus + c.Status.String()}})
		}
		if c.Priority != nil {
			put(fieldPriority, Value{Enum: []string{prefixPriority + string(*c.Priority)}})
		}
		if c.Owner != "" {
			put(fieldOwner, Value{Text: c.Owner})
		}
		if len(c.Labels) > 0 {
			put(fieldLabels, Value{Text: strings.Join(c.Labels, ", ")})
		}
		if len(c.Test.Preconditions) > 0 {
			put(fieldPreconditions, Value{XHTML: FromDocument(c.Test.Preconditions, b.fileRef)})
		}
		if len(c.Test.Steps) > 0 {
			put(fieldSteps, Value{XHTML: FromDocument(requirement.StepsTable(c.Test.Steps, "Action", "Expected result"), b.fileRef)})
		}
		if len(c.Test.Parameters) > 0 {
			put(fieldParameters, Value{Text: requirement.ParametersText(c.Test.Parameters)})
		}
		for _, d := range b.snap.defs {
			if v, ok := b.attributeValue(d, c.Attributes[d.ID.String()]); ok {
				if def := defs[attrKey(d.ID)]; def != nil {
					v.Def = def.Identifier
					o.Values = append(o.Values, v)
				}
			}
		}
	}
	b.file.Objects = append(b.file.Objects, o)
}

// attributeValue writes a stored value as ReqIF holds it; an empty or unreadable one is none.
func (b *builder) attributeValue(d attribute.Def, raw json.RawMessage) (Value, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return Value{}, false
	}
	var (
		s  string
		n  float64
		ok bool
	)
	switch d.Kind {
	case attribute.Text, attribute.URL:
		ok = json.Unmarshal(raw, &s) == nil
		return Value{Text: s}, ok && s != ""
	case attribute.User:
		ok = json.Unmarshal(raw, &s) == nil
		return Value{Text: b.snap.people[s]}, ok && b.snap.people[s] != ""
	case attribute.RichText:
		return Value{XHTML: FromDocument(raw, b.fileRef)}, true
	case attribute.Number:
		ok = json.Unmarshal(raw, &n) == nil
		return Value{Text: strconv.FormatFloat(n, 'g', -1, 64)}, ok
	case attribute.Boolean:
		var v bool
		ok = json.Unmarshal(raw, &v) == nil
		return Value{Text: strconv.FormatBool(v)}, ok
	case attribute.Date:
		day, err := time.Parse(time.DateOnly, strings.Trim(string(raw), `"`))
		return Value{Text: day.Format(dateLayout)}, err == nil
	case attribute.Select, attribute.MultiSelect:
		var ids []string
		if json.Unmarshal(raw, &s) == nil {
			ids = []string{s}
		} else if json.Unmarshal(raw, &ids) != nil {
			return Value{}, false
		}
		var refs []string
		for _, id := range ids {
			if parsed, err := uuid.Parse(id); err == nil {
				refs = append(refs, b.snap.identifier("option", parsed, nil, prefixOption+parsed.String()))
			}
		}
		return Value{Enum: refs}, len(refs) > 0
	}
	return Value{}, false
}

func (b *builder) relations() {
	for _, l := range b.snap.links {
		b.file.Relations = append(b.file.Relations, &Relation{
			Identity: Identity{Identifier: prefixTrace + l.ID.String(), Changed: l.Created},
			Type:     b.snap.identifier("relation_type", l.Type, nil, prefixLinkType+l.Type.String()),
			Source:   b.objectID(l.Source), Target: b.objectID(l.Target),
		})
	}
}

// specification writes the tree: the project's, or below a section a specification of an exchange became.
func (b *builder) specification() {
	identifier := b.snap.identifier("specification", b.snap.project.ID, nil, prefixProject+b.snap.project.ID.String())
	nodes := b.snap.nodes
	if root := b.snap.root; root != nil {
		if tied := b.snap.identifier("specification", root.ID, nil, ""); tied != "" {
			identifier, nodes = tied, nodes[1:]
		}
	}
	spec := &Specification{Identity: b.id(identifier, b.file.Title), Type: specificationType}
	byID := map[uuid.UUID]*Node{}
	for _, n := range nodes {
		node := &Node{Identity: b.id(prefixNode+n.ID.String(), ""), Object: b.objectID(n.ID)}
		byID[n.ID] = node
		if n.ParentID != nil && byID[*n.ParentID] != nil {
			parent := byID[*n.ParentID]
			parent.Children = append(parent.Children, node)
		} else {
			spec.Children = append(spec.Children, node)
		}
	}
	b.file.Specifications = append(b.file.Specifications, spec)
}

// glossary writes the terms as objects of a spec type of their own, listed by a second specification.
func (b *builder) glossary() {
	if len(b.snap.glossary) == 0 {
		return
	}
	defs := []*AttrDef{
		{Identity: b.id(termName, "Name"), Kind: KindString, DataType: dataString},
		{Identity: b.id(termDefinition, "Definition"), Kind: KindXHTML, DataType: dataXHTML},
		{Identity: b.id(termSynonyms, "Synonyms"), Kind: KindString, DataType: dataString},
		{Identity: b.id(termAvoid, "Avoid"), Kind: KindString, DataType: dataString},
		{Identity: b.id(termStatus, "Status"), Kind: KindString, DataType: dataString},
	}
	b.file.Types = append(b.file.Types,
		&SpecType{Identity: b.id(glossaryTermType, "Glossary term"), Kind: TypeObject, Attributes: defs},
		&SpecType{Identity: b.id(glossarySpecType, "Glossary"), Kind: TypeSpecification})
	spec := &Specification{Identity: b.id(prefixGlossary+b.snap.project.ID.String(), "Glossary"), Type: glossarySpecType}
	for _, t := range b.snap.glossary {
		o := &Object{Identity: b.id(prefixTerm+t.ID.String(), ""), Type: glossaryTermType}
		o.Values = append(o.Values, Value{Def: termName, Text: t.Name}, Value{Def: termDefinition, XHTML: FromDocument(t.Definition, b.fileRef)})
		if len(t.Synonyms) > 0 {
			o.Values = append(o.Values, Value{Def: termSynonyms, Text: strings.Join(t.Synonyms, ", ")})
		}
		if len(t.Avoid) > 0 {
			o.Values = append(o.Values, Value{Def: termAvoid, Text: strings.Join(t.Avoid, ", ")})
		}
		o.Values = append(o.Values, Value{Def: termStatus, Text: t.Status})
		b.file.Objects = append(b.file.Objects, o)
		spec.Children = append(spec.Children, &Node{Identity: b.id(prefixTermNode+t.ID.String(), ""), Object: o.Identifier})
	}
	b.file.Specifications = append(b.file.Specifications, spec)
}
