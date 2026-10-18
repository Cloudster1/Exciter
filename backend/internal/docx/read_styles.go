package docx

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// style is a paragraph or character style as the document defines it.
type style struct {
	id, name, basedOn string
	// outline is the outline level it sets, -1 for none.
	outline int
	num     *numRef
}

// numRef is a paragraph's numbering: the instance and its level.
type numRef struct {
	numID string
	ilvl  int
	// hasLevel says the level was given, which a style's numbering may leave to the paragraph.
	hasLevel bool
}

// level is how one level of a numbering counts and what it shows.
type level struct {
	start  int
	format string
	text   string
	legal  bool
}

// numbering is an instance of an abstract numbering, with the levels it overrides.
type numbering struct {
	abstract  string
	overrides map[int]level
	restarts  map[int]int
}

// counter is the count of each level of one list of numbers.
type counter struct {
	value [maxListLevel + 1]int
	used  [maxListLevel + 1]bool
}

// headingName is the name Word gives its heading styles, in English whatever the document's language.
var headingName = regexp.MustCompile(`^heading ([1-9])$`)

// maxHeadingLevel is the deepest heading Word has a style for.
const maxHeadingLevel = 9

// bodyOutline is the outline level of body text.
const bodyOutline = 9

func (r *reader) readStyles() error {
	r.styles = map[string]*style{}
	data, err := r.pkg.part(r.related("styles", "styles.xml"))
	if err != nil {
		return err
	}
	root, err := parse(data)
	if err != nil || root == nil {
		return nil
	}
	for _, n := range root.kids {
		if n.local() != "style" {
			continue
		}
		s := &style{id: n.attr("styleId"), outline: -1}
		if name := n.child("name"); name != nil {
			s.name = name.attr("val")
		}
		if based := n.child("basedOn"); based != nil {
			s.basedOn = based.attr("val")
		}
		if pPr := n.child("pPr"); pPr != nil {
			if o := pPr.child("outlineLvl"); o != nil {
				if v, err := strconv.Atoi(o.attr("val")); err == nil {
					s.outline = v
				}
			}
			s.num = numOf(pPr.child("numPr"))
		}
		if s.name == "" {
			s.name = s.id
		}
		r.styles[s.id] = s
		if def := n.attr("default"); n.attr("type") == "paragraph" && (def == "1" || strings.EqualFold(def, "true") || strings.EqualFold(def, "on")) {
			r.defaultStyle = s.id
		}
	}
	return nil
}

// numOf reads a numPr; nil is none, and numId 0 takes numbering away.
func numOf(n *node) *numRef {
	if n == nil {
		return nil
	}
	ref := &numRef{}
	if id := n.child("numId"); id != nil {
		ref.numID = id.attr("val")
	}
	if l := n.child("ilvl"); l != nil {
		if v, err := strconv.Atoi(l.attr("val")); err == nil && v >= 0 && v <= maxListLevel {
			ref.ilvl, ref.hasLevel = v, true
		}
	}
	return ref
}

// chain is a style and the styles it is based on, nearest first, without going round.
func (r *reader) chain(id string) []*style {
	var out []*style
	seen := map[string]bool{}
	for id != "" && !seen[id] {
		seen[id] = true
		s := r.styles[id]
		if s == nil {
			break
		}
		out = append(out, s)
		id = s.basedOn
	}
	return out
}

// headingLevel is the level a paragraph of the style heads at, 0 for none: the nearest outline level a style sets
// or a heading style's name, whichever comes first.
func (r *reader) headingLevel(styleID string, direct int) int {
	if direct >= 0 {
		return outlineHeading(direct)
	}
	for _, s := range r.chain(styleID) {
		if s.outline >= 0 {
			return outlineHeading(s.outline)
		}
		if m := headingName.FindStringSubmatch(strings.ToLower(s.name)); m != nil {
			n, _ := strconv.Atoi(m[1])
			return n
		}
	}
	return 0
}

func outlineHeading(outline int) int {
	if outline < 0 || outline >= bodyOutline {
		return 0
	}
	return outline + 1
}

// styleNum is the numbering a paragraph's style gives it, nearest first.
func (r *reader) styleNum(styleID string) *numRef {
	for _, s := range r.chain(styleID) {
		if s.num != nil {
			return s.num
		}
	}
	return nil
}

func (r *reader) styleName(id string) string {
	if s := r.styles[id]; s != nil {
		return s.name
	}
	return id
}

// styleNamed says whether the style or one it is based on has one of the names, in any case.
func (r *reader) styleNamed(id string, names ...string) bool {
	for _, s := range r.chain(id) {
		for _, name := range names {
			if strings.EqualFold(s.name, name) || strings.EqualFold(s.id, name) {
				return true
			}
		}
	}
	return false
}

func (r *reader) readNumbering() error {
	r.abstracts, r.nums = map[string]map[int]level{}, map[string]*numbering{}
	data, err := r.pkg.part(r.related("numbering", "numbering.xml"))
	if err != nil {
		return err
	}
	root, err := parse(data)
	if err != nil || root == nil {
		return nil
	}
	for _, n := range root.kids {
		switch n.local() {
		case "abstractNum":
			levels := map[int]level{}
			for _, l := range n.kids {
				if l.local() != "lvl" {
					continue
				}
				if i, err := strconv.Atoi(l.attr("ilvl")); err == nil && i >= 0 && i <= maxListLevel {
					levels[i] = levelOf(l)
				}
			}
			r.abstracts[n.attr("abstractNumId")] = levels
		case "num":
			num := &numbering{overrides: map[int]level{}, restarts: map[int]int{}}
			if a := n.child("abstractNumId"); a != nil {
				num.abstract = a.attr("val")
			}
			for _, o := range n.kids {
				if o.local() != "lvlOverride" {
					continue
				}
				i, err := strconv.Atoi(o.attr("ilvl"))
				if err != nil || i < 0 || i > maxListLevel {
					continue
				}
				if s := o.child("startOverride"); s != nil {
					if v, err := strconv.Atoi(s.attr("val")); err == nil {
						num.restarts[i] = v
					}
				}
				if l := o.child("lvl"); l != nil {
					num.overrides[i] = levelOf(l)
				}
			}
			r.nums[n.attr("numId")] = num
		}
	}
	return nil
}

func levelOf(l *node) level {
	lv := level{start: 1, format: "decimal"}
	if s := l.child("start"); s != nil {
		if v, err := strconv.Atoi(s.attr("val")); err == nil {
			lv.start = v
		}
	}
	if f := l.child("numFmt"); f != nil {
		lv.format = f.attr("val")
	}
	if t := l.child("lvlText"); t != nil {
		lv.text = t.attr("val")
	}
	lv.legal = on(l.child("isLgl"))
	return lv
}

// levelOfNum is how a level of a numbering instance counts, its own override first.
func (r *reader) levelOfNum(num *numbering, ilvl int) (level, bool) {
	if l, ok := num.overrides[ilvl]; ok {
		return l, true
	}
	l, ok := r.abstracts[num.abstract][ilvl]
	if start, restarted := num.restarts[ilvl]; ok && restarted {
		l.start = start
	}
	return l, ok
}

// numbered counts a paragraph of a numbering and answers what it shows and how its level counts. Instances that
// restart count on their own; the others share their abstract numbering's count, as Word counts them.
func (r *reader) numbered(ref *numRef) (label string, lv level, ok bool) {
	num := r.nums[ref.numID]
	if num == nil || ref.numID == "0" {
		return "", level{}, false
	}
	lv, ok = r.levelOfNum(num, ref.ilvl)
	if !ok {
		return "", level{}, false
	}
	key := "abstract:" + num.abstract
	if len(num.restarts) > 0 || len(num.overrides) > 0 {
		key = "num:" + ref.numID
	}
	c := r.counters[key]
	if c == nil {
		c = &counter{}
		r.counters[key] = c
	}
	if c.used[ref.ilvl] {
		c.value[ref.ilvl]++
	} else {
		c.value[ref.ilvl], c.used[ref.ilvl] = lv.start, true
	}
	for deeper := ref.ilvl + 1; deeper <= maxListLevel; deeper++ {
		c.used[deeper] = false
	}
	if lv.format == "bullet" || lv.format == "none" {
		return lv.text, lv, true
	}
	var b strings.Builder
	text := lv.text
	for i := 0; i < len(text); i++ {
		if text[i] == '%' && i+1 < len(text) && text[i+1] >= '1' && text[i+1] <= '9' {
			at := int(text[i+1] - '1')
			i++
			shown, ok := r.levelOfNum(num, at)
			if !ok {
				continue
			}
			value := c.value[at]
			if !c.used[at] {
				value = shown.start
			}
			format := shown.format
			if lv.legal {
				format = "decimal"
			}
			b.WriteString(formatNumber(value, format))
			continue
		}
		b.WriteByte(text[i])
	}
	return b.String(), lv, true
}

// formatNumber writes a count as a numbering format shows it; a format a reader does not know counts in digits.
func formatNumber(n int, format string) string {
	switch format {
	case "lowerLetter":
		return letters(n, 'a')
	case "upperLetter":
		return letters(n, 'A')
	case "lowerRoman":
		return strings.ToLower(roman(n))
	case "upperRoman":
		return roman(n)
	case "decimalZero":
		if n >= 0 && n < 10 {
			return "0" + strconv.Itoa(n)
		}
	}
	return strconv.Itoa(n)
}

// letters counts as Word does past z: aa, bb, and so on.
func letters(n int, first rune) string {
	if n <= 0 {
		return strconv.Itoa(n)
	}
	const alphabet = 26
	letter := rune(int(first) + (n-1)%alphabet)
	return strings.Repeat(string(letter), (n-1)/alphabet+1)
}

var romanDigits = []struct {
	value  int
	digits string
}{{1000, "M"}, {900, "CM"}, {500, "D"}, {400, "CD"}, {100, "C"}, {90, "XC"}, {50, "L"}, {40, "XL"}, {10, "X"}, {9, "IX"}, {5, "V"}, {4, "IV"}, {1, "I"}}

func roman(n int) string {
	if n <= 0 {
		return strconv.Itoa(n)
	}
	var b strings.Builder
	for _, d := range romanDigits {
		for n >= d.value {
			b.WriteString(d.digits)
			n -= d.value
		}
	}
	return b.String()
}

// placeholder is where a level's text shows a count.
var placeholder = regexp.MustCompile(`%[1-9]`)

// isLabel says a level's text names something rather than counting a list, as REQ-%1 does and %1. does not.
func isLabel(lv level) bool {
	if lv.format == "bullet" {
		return false
	}
	return strings.IndexFunc(placeholder.ReplaceAllString(lv.text, ""), unicode.IsLetter) >= 0
}
