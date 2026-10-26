package formula

import (
	"regexp"
	"slices"
	"strings"

	"github.com/google/uuid"
)

// Attribute is an attribute a formula may read, with the kind of value it reads as.
type Attribute struct {
	ID   uuid.UUID
	Name string
	Kind Kind
}

// LinkType is a link type a formula may follow, by its key.
type LinkType struct {
	ID      uuid.UUID
	Key     string
	Acyclic bool
}

// Catalog is what a project offers a formula: its attributes and link types, and the attribute being defined, which
// a formula may not read of the same requirement.
type Catalog struct {
	Attributes []Attribute
	LinkTypes  []LinkType
	Self       uuid.UUID
}

// LinkRead is a link type followed one way, and the attributes read of the requirements it leads to.
type LinkRead struct {
	TypeID     uuid.UUID
	Out        bool
	Attributes []uuid.UUID
}

// Reads is what a formula reads: of the requirement itself, of its children, and along its links.
type Reads struct {
	Self          []uuid.UUID
	ReadsChildren bool
	Children      []uuid.UUID
	Links         []LinkRead
	// Today is a call of today(), whose value moves on each day without any change.
	Today bool
}

// Program is a formula checked against a project: what it gives, what it reads, and its text as kept.
type Program struct {
	root node
	// ids are the attributes and link types the formula names, by the character their name starts at.
	ids    map[int]uuid.UUID
	Kind   Kind
	Reads  Reads
	stored string
}

// Stored is the formula as it is kept: attributes and link types named by id, so renaming either keeps it.
func (p *Program) Stored() string { return p.stored }

var idRef = regexp.MustCompile(`(["'])@([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})(["'])`)

// Display spells a kept formula for a person, with the names attributes and link types have now.
func Display(stored string, cat Catalog) string {
	return idRef.ReplaceAllStringFunc(stored, func(m string) string {
		parts := idRef.FindStringSubmatch(m)
		id, err := uuid.Parse(parts[2])
		if err != nil || parts[1] != parts[3] {
			return m
		}
		if parts[1] == `"` {
			if i := slices.IndexFunc(cat.Attributes, func(a Attribute) bool { return a.ID == id }); i >= 0 {
				return `"` + strings.ReplaceAll(cat.Attributes[i].Name, `"`, `\"`) + `"`
			}
			return m
		}
		if i := slices.IndexFunc(cat.LinkTypes, func(l LinkType) bool { return l.ID == id }); i >= 0 {
			return "'" + cat.LinkTypes[i].Key + "'"
		}
		return m
	})
}

type checker struct {
	cat     Catalog
	reads   Reads
	inItems *set
	// spans are the attribute names and link type keys written, to be kept by id.
	spans []replacement
	ids   map[int]uuid.UUID
}

type replacement struct {
	from, to int
	text     string
}

// Compile parses a formula and checks it against a project: every name known, every value of a kind its use takes.
func Compile(text string, cat Catalog) (*Program, *Error) {
	root, err := parse(text)
	if err != nil {
		return nil, err
	}
	c := &checker{cat: cat, ids: map[int]uuid.UUID{}}
	kind, err := c.check(root)
	if err != nil {
		return nil, err
	}
	if kind == "" {
		from, to := root.span()
		return nil, errorAt(from, to, "A formula must give a value, and null alone gives none.")
	}
	runes := []rune(text)
	slices.SortFunc(c.spans, func(a, b replacement) int { return b.from - a.from })
	for _, r := range c.spans {
		runes = slices.Concat(runes[:r.from-1], []rune(r.text), runes[r.to:])
	}
	return &Program{root: root, ids: c.ids, Kind: kind, Reads: c.reads, stored: string(runes)}, nil
}

// byRef reads a name as written, or an id kept as "@id".
func byRef(name string) (uuid.UUID, bool) {
	if strings.HasPrefix(name, "@") {
		id, err := uuid.Parse(name[1:])
		return id, err == nil
	}
	return uuid.Nil, false
}

func (c *checker) attribute(r attrRef) (Attribute, *Error) {
	id, byID := byRef(r.name)
	for _, a := range c.cat.Attributes {
		if (byID && a.ID == id) || (!byID && strings.EqualFold(strings.TrimSpace(a.Name), strings.TrimSpace(r.name))) {
			return a, nil
		}
	}
	return Attribute{}, errorAt(r.from, r.to, "There is no attribute %q in this project. Name one of its attributes in double quotes.", r.name)
}

func (c *checker) linkType(s set) (LinkType, *Error) {
	id, byID := byRef(s.typeKey)
	for _, l := range c.cat.LinkTypes {
		if (byID && l.ID == id) || (!byID && l.Key == s.typeKey) {
			return l, nil
		}
	}
	return LinkType{}, errorAt(s.from, s.to, "There is no link type %q in this project. Name one by its key, such as 'verifies'.", s.typeKey)
}

func (c *checker) note(id uuid.UUID) {
	switch {
	case c.inItems == nil:
		if !slices.Contains(c.reads.Self, id) {
			c.reads.Self = append(c.reads.Self, id)
		}
	case c.inItems.children:
		if !slices.Contains(c.reads.Children, id) {
			c.reads.Children = append(c.reads.Children, id)
		}
	default:
		i := slices.IndexFunc(c.reads.Links, func(l LinkRead) bool { return l.TypeID == c.inItems.typeID && l.Out == c.inItems.out })
		if !slices.Contains(c.reads.Links[i].Attributes, id) {
			c.reads.Links[i].Attributes = append(c.reads.Links[i].Attributes, id)
		}
	}
}

var kindNames = map[Kind]string{Number: "a number", Text: "text", Boolean: "true or false", Date: "a date", "": "null"}

// want refuses a value of another kind than one asked; null fits any.
func want(n node, got Kind, kinds ...Kind) *Error {
	if got == "" || slices.Contains(kinds, got) {
		return nil
	}
	names := make([]string, len(kinds))
	for i, k := range kinds {
		names[i] = kindNames[k]
	}
	from, to := n.span()
	return errorAt(from, to, "This is %s, where %s belongs.", kindNames[got], strings.Join(names, " or "))
}

// same is the kind two values share, null taking the other's.
func same(a node, ka Kind, b node, kb Kind) (Kind, *Error) {
	switch {
	case ka == "":
		return kb, nil
	case kb == "" || ka == kb:
		return ka, nil
	}
	from, _ := a.span()
	_, to := b.span()
	return "", errorAt(from, to, "These are %s and %s, which do not go together.", kindNames[ka], kindNames[kb])
}

func (c *checker) check(n node) (Kind, *Error) {
	switch v := n.(type) {
	case numberLit:
		return Number, nil
	case textLit:
		return Text, nil
	case boolLit:
		return Boolean, nil
	case nullLit:
		return "", nil
	case fieldRef:
		return Fields[v.name], nil
	case attrRef:
		a, err := c.attribute(v)
		if err != nil {
			return "", err
		}
		if c.inItems == nil && a.ID == c.cat.Self {
			return "", errorAt(v.from, v.to, "A formula cannot read its own attribute of the same requirement. Read it of the children or along a link instead.")
		}
		c.note(a.ID)
		c.ids[v.from] = a.ID
		c.spans = append(c.spans, replacement{v.from, v.to, `"@` + a.ID.String() + `"`})
		return a.Kind, nil
	case unary:
		k, err := c.check(v.x)
		if err != nil {
			return "", err
		}
		if v.op == "not" {
			return Boolean, want(v.x, k, Boolean)
		}
		return Number, want(v.x, k, Number)
	case binary:
		return c.binary(v)
	case call:
		return c.call(v)
	case aggregate:
		return c.aggregate(v)
	}
	return "", nil
}

func (c *checker) binary(v binary) (Kind, *Error) {
	kx, err := c.check(v.x)
	if err != nil {
		return "", err
	}
	ky, err := c.check(v.y)
	if err != nil {
		return "", err
	}
	switch v.op {
	case "and", "or":
		if err := want(v.x, kx, Boolean); err != nil {
			return "", err
		}
		return Boolean, want(v.y, ky, Boolean)
	case "+", "-", "*", "/":
		if err := want(v.x, kx, Number); err != nil {
			return "", err
		}
		return Number, want(v.y, ky, Number)
	case "<", "<=", ">", ">=":
		if err := want(v.x, kx, Number, Date, Text); err != nil {
			return "", err
		}
	}
	_, err = same(v.x, kx, v.y, ky)
	return Boolean, err
}

func (c *checker) call(v call) (Kind, *Error) {
	kinds := make([]Kind, len(v.args))
	for i, a := range v.args {
		k, err := c.check(a)
		if err != nil {
			return "", err
		}
		kinds[i] = k
	}
	all := func(allowed ...Kind) *Error {
		for i, a := range v.args {
			if err := want(a, kinds[i], allowed...); err != nil {
				return err
			}
		}
		return nil
	}
	switch v.fn {
	case "if":
		if err := want(v.args[0], kinds[0], Boolean); err != nil {
			return "", err
		}
		return same(v.args[1], kinds[1], v.args[2], kinds[2])
	case "coalesce":
		k := kinds[0]
		for i := 1; i < len(v.args); i++ {
			var err *Error
			if k, err = same(v.args[0], k, v.args[i], kinds[i]); err != nil {
				return "", err
			}
		}
		return k, nil
	case "round", "floor", "ceil", "abs", "min", "max":
		return Number, all(Number)
	case "concat":
		return Text, nil
	case "lower", "upper":
		return Text, all(Text)
	case "len":
		return Number, all(Text)
	case "today":
		c.reads.Today = true
		return Date, nil
	case "days":
		return Number, all(Date)
	}
	return "", nil
}

func (c *checker) aggregate(v aggregate) (Kind, *Error) {
	over := v.over
	if !over.children {
		l, err := c.linkType(over)
		if err != nil {
			return "", err
		}
		over.typeID = l.ID
		c.ids[over.from] = l.ID
		c.spans = append(c.spans, replacement{over.from, over.to, "'@" + l.ID.String() + "'"})
		if !slices.ContainsFunc(c.reads.Links, func(r LinkRead) bool { return r.TypeID == l.ID && r.Out == over.out }) {
			c.reads.Links = append(c.reads.Links, LinkRead{TypeID: l.ID, Out: over.out, Attributes: []uuid.UUID{}})
		}
	} else {
		c.reads.ReadsChildren = true
	}
	c.inItems = &over
	defer func() { c.inItems = nil }()
	if v.where != nil {
		k, err := c.check(v.where)
		if err != nil {
			return "", err
		}
		if err := want(v.where, k, Boolean); err != nil {
			return "", err
		}
	}
	if v.value == nil {
		return Number, nil
	}
	k, err := c.check(v.value)
	if err != nil {
		return "", err
	}
	switch v.fn {
	case "any", "all":
		return Boolean, want(v.value, k, Boolean)
	case "min", "max":
		if err := want(v.value, k, Number, Date); err != nil {
			return "", err
		}
		if k == "" {
			return Number, nil
		}
		return k, nil
	}
	return Number, want(v.value, k, Number)
}
