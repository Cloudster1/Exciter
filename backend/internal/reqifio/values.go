package reqifio

import (
	"encoding/json"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/attribute"
	"github.com/cloudster1/exciter/backend/internal/requirement"
)

func encode(v any) json.RawMessage {
	out, _ := json.Marshal(v)
	return out
}

// planValue reads one value of an object into its plan: a field, or an attribute's value.
func (a *analysis) planValue(p *objectPlan, o *Object, v *Value, name, chapter *string) {
	d := a.index.defs[v.Def]
	t := a.schema.attrs[v.Def]
	if d == nil || t == nil {
		a.lose(LossValue, p.Where, "A value of it belongs to no attribute its type defines, so it is left out.")
		return
	}
	section := p.Kind == requirement.KindSection
	switch t.field {
	case fieldKey:
	case fieldTitle:
		*name = textOf(v)
	case fieldChapter:
		*chapter = textOf(v)
	case fieldStatement:
		p.Statement = a.document(p, fieldStatement, v)
	case fieldRationale:
		if section {
			a.lose(LossValue, p.Where, "Sections have no rationale, so its rationale is left out.")
			return
		}
		p.Rationale = a.document(p, fieldRationale, v)
	case fieldStatus:
		if !section {
			a.planStatus(p, v)
		}
	case fieldPriority:
		if !section {
			a.planPriority(p, v)
		}
	case fieldOwner:
		if !section {
			a.planOwner(p, v)
		}
	case fieldLabels:
		for _, label := range strings.Split(textOf(v), ",") {
			if label = strings.TrimSpace(label); label != "" {
				p.Labels = append(p.Labels, label)
			}
		}
	default:
		if t.skip || t.lost {
			return
		}
		if section {
			a.lose(LossValue, p.Where, "Sections keep no attributes, so its %s is left out.", t.item.Name)
			return
		}
		a.planAttribute(p, t, v)
	}
}

// document reads a rich or plain text value as a document, keeping the rich text to read again with its files.
func (a *analysis) document(p *objectPlan, key string, v *Value) json.RawMessage {
	if v.XHTML == nil {
		return PlainDocument(v.Text)
	}
	p.rich[key] = v.XHTML
	doc, losses := ToDocument(v.XHTML, a.placer)
	for _, l := range losses {
		a.lose(l.kind, p.Where, "%s", l.message)
	}
	return doc
}

func (a *analysis) enumValue(v *Value) *EnumValue {
	if len(v.Enum) == 0 {
		return nil
	}
	return a.index.enums[v.Enum[0]]
}

// planStatus sets the status of what an import makes; one that exists keeps its own, as a status moves by
// transitions alone.
func (a *analysis) planStatus(p *objectPlan, v *Value) {
	ev := a.enumValue(v)
	if ev == nil {
		return
	}
	var status *uuid.UUID
	if id, ok := strings.CutPrefix(ev.Identifier, prefixStatus); ok {
		if parsed, err := uuid.Parse(id); err == nil && a.world.statuses[parsed].ID != uuid.Nil {
			status = &parsed
		}
	}
	if status == nil {
		if s, ok := a.world.statusNamed(nameOf(ev.Identity)); ok {
			status = &s.ID
		}
	}
	switch {
	case status == nil:
		a.lose(LossStatus, p.Where, "The status %s is not one of the project's, so it starts in the first status of its workflow.", nameOf(ev.Identity))
	case p.Existing != nil:
		if p.Existing.Status == nil || *p.Existing.Status != *status {
			a.lose(LossStatus, p.Where, "It keeps its status in the project; a status moves by transitions, not by an import.")
		}
	default:
		workflow := uuid.Nil
		if p.TypeID != nil {
			workflow = *p.TypeID
		}
		if !a.world.workflow[workflow][*status] {
			a.lose(LossStatus, p.Where, "The status %s is not in the workflow of its type, so it starts in the first status of the workflow.", nameOf(ev.Identity))
			return
		}
		p.Status = status
	}
}

func (a *analysis) planPriority(p *objectPlan, v *Value) {
	ev := a.enumValue(v)
	if ev == nil {
		return
	}
	for _, candidate := range []string{strings.TrimPrefix(ev.Identifier, prefixPriority), strings.ToLower(nameOf(ev.Identity))} {
		priority := requirement.Priority(strings.ReplaceAll(candidate, "'", ""))
		if slices.Contains(requirement.Priorities, priority) {
			p.Priority = &priority
			return
		}
	}
	a.lose(LossValue, p.Where, "The priority %s is none of must, should, could and won't, so it is left out.", nameOf(ev.Identity))
}

func (a *analysis) planOwner(p *objectPlan, v *Value) {
	email := strings.ToLower(textOf(v))
	p.OwnerSet = true
	if email == "" {
		return
	}
	if id, ok := a.world.members[email]; ok {
		p.Owner = &id
		return
	}
	p.OwnerSet = p.Existing == nil
	a.lose(LossPerson, p.Where, "Its owner %s is no member who may view the project, so it is left without one.", email)
}

// planAttribute turns a value into the stored form of the attribute it maps to.
func (a *analysis) planAttribute(p *objectPlan, t *attrTarget, v *Value) {
	var key string
	if t.def != nil {
		key = t.def.ID.String()
	} else if t.create != nil && t.create.made != nil {
		key = t.create.made.ID.String()
	} else {
		key = "new:" + t.item.Identifier
	}
	name := t.item.Name
	switch t.kind() {
	case attribute.Text:
		s := textOf(v)
		if utf8.RuneCountInString(s) > attribute.MaxTextLength {
			a.problem(p.Where, "Its %s is longer than the %d characters a text attribute holds. Map %s to a rich text attribute, or leave it out.", name, attribute.MaxTextLength, name)
			return
		}
		if s != "" {
			p.Attributes[key] = encode(s)
		}
	case attribute.RichText:
		p.Attributes[key] = a.document(p, key, v)
	case attribute.Number:
		n, err := strconv.ParseFloat(strings.TrimSpace(v.Text), 64)
		if err != nil {
			a.lose(LossValue, p.Where, "Its %s, %q, is no number, so it is left out.", name, v.Text)
			return
		}
		p.Attributes[key] = encode(n)
	case attribute.Boolean:
		switch strings.ToLower(strings.TrimSpace(v.Text)) {
		case "true", "1":
			p.Attributes[key] = encode(true)
		case "false", "0":
			p.Attributes[key] = encode(false)
		default:
			a.lose(LossValue, p.Where, "Its %s, %q, is neither true nor false, so it is left out.", name, v.Text)
		}
	case attribute.Date:
		day := parseTime(v.Text)
		if day.IsZero() {
			a.lose(LossValue, p.Where, "Its %s, %q, is no date, so it is left out.", name, v.Text)
			return
		}
		p.Attributes[key] = encode(day.Format(time.DateOnly))
	case attribute.Select, attribute.MultiSelect:
		a.planOptions(p, t, key, v)
	case attribute.User:
		email := strings.ToLower(textOf(v))
		if id, ok := a.world.members[email]; ok {
			p.Attributes[key] = encode(id.String())
		} else if email != "" {
			a.lose(LossPerson, p.Where, "Its %s names %s, who is no member who may view the project, so it is left out.", name, email)
		}
	case attribute.URL:
		s := textOf(v)
		if u, err := url.Parse(s); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			a.lose(LossValue, p.Where, "Its %s, %q, is no web address, so it is left out.", name, s)
			return
		}
		p.Attributes[key] = encode(s)
	}
}

func (a *analysis) planOptions(p *objectPlan, t *attrTarget, key string, v *Value) {
	var ids []string
	for _, ref := range v.Enum {
		ev := a.index.enums[ref]
		if ev == nil {
			continue
		}
		ids = append(ids, a.optionFor(t, ev))
	}
	if len(ids) == 0 {
		return
	}
	if t.kind() == attribute.Select {
		if len(ids) > 1 {
			a.lose(LossValue, p.Where, "Its %s has several values and takes one, so it keeps the first.", t.item.Name)
		}
		p.Attributes[key] = encode(ids[0])
		return
	}
	if len(ids) > attribute.MaxMultiSelect {
		a.lose(LossValue, p.Where, "Its %s has more than %d values, so it keeps the first %d.", t.item.Name, attribute.MaxMultiSelect, attribute.MaxMultiSelect)
		ids = ids[:attribute.MaxMultiSelect]
	}
	p.Attributes[key] = encode(ids)
}

// optionFor is the id of the option an enumeration value is: tied by an earlier exchange, or of its name. One the
// attribute lacks is added when the import runs, and stands in as nil until then.
func (a *analysis) optionFor(t *attrTarget, ev *EnumValue) string {
	def := t.def
	if def == nil && t.create != nil {
		def = t.create.made
	}
	if def == nil {
		return uuid.Nil.String()
	}
	if tie, ok := a.world.tied("option", ev.Identifier, prefixOption); ok {
		for _, o := range def.Options {
			if o.ID == tie.Target && !o.Retired {
				return o.ID.String()
			}
		}
	}
	name := strings.TrimSpace(truncate(nameOf(ev.Identity), attribute.MaxNameLength))
	for _, o := range def.Options {
		if strings.EqualFold(o.Name, name) && !o.Retired {
			return o.ID.String()
		}
	}
	t.missing = appendNew(t.missing, name)
	return uuid.Nil.String()
}

// checkRequired refuses a new requirement without a value its type requires.
func (a *analysis) checkRequired(p *objectPlan) {
	for _, d := range a.world.defs {
		if !d.Required || d.Retired || !d.Shows(p.TypeID) {
			continue
		}
		if _, ok := p.Attributes[d.ID.String()]; !ok {
			a.problem(p.Where, "It has no %s, which the project requires of its type. Map an attribute of the file to %s, or give it a value in the file.", d.Name, d.Name)
		}
	}
}
