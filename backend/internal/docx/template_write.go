package docx

import (
	"encoding/xml"
	"path"
	"regexp"
	"strconv"
	"strings"
)

// How an export is written into a template: the template's styles kept, the export's added where it lacks them,
// its numbering after the template's, and the template's parts beside the export's.

// builtinStyles are the export's styles Word names the same in every template, whatever language it is in.
var builtinStyles = map[string]string{
	"Normal": "normal", "Title": "title", "Subtitle": "subtitle", "TOCHeading": "toc heading", "Hyperlink": "hyperlink",
	"Heading1": "heading 1", "Heading2": "heading 2", "Heading3": "heading 3", "Heading4": "heading 4", "Heading5": "heading 5",
	"Heading6": "heading 6", "TOC1": "toc 1", "TOC2": "toc 2", "TOC3": "toc 3",
}

var (
	styleRe     = regexp.MustCompile(`(?s)<w:style\b[^>]*\bw:styleId="([^"]+)"[^>]*>.*?</w:style>`)
	styleNameRe = regexp.MustCompile(`<w:name w:val="([^"]+)"`)
)

// mergeStyles keeps the template's styles, adds the export's it has no style of, and answers which of the export's
// style ids write as the template's.
func mergeStyles(template, ours []byte) ([]byte, map[string]string) {
	ids := map[string]string{}
	byName, byID := map[string]string{}, map[string]bool{}
	for _, m := range styleRe.FindAllStringSubmatch(string(template), -1) {
		byID[m[1]] = true
		if n := styleNameRe.FindStringSubmatch(m[0]); n != nil {
			byName[strings.ToLower(n[1])] = m[1]
		}
	}
	var added strings.Builder
	for _, m := range styleRe.FindAllStringSubmatch(string(ours), -1) {
		id := m[1]
		if name, builtin := builtinStyles[id]; builtin {
			if theirs, ok := byName[name]; ok {
				ids[id] = theirs
				continue
			}
		}
		if byID[id] {
			continue
		}
		added.WriteString(m[0])
	}
	text := string(template)
	at := strings.LastIndex(text, "</w:styles>")
	if at < 0 {
		return ours, ids
	}
	return []byte(text[:at] + added.String() + text[at:]), ids
}

var (
	abstractNumRe   = regexp.MustCompile(`(?s)<w:abstractNum\b.*?</w:abstractNum>`)
	numRe           = regexp.MustCompile(`(?s)<w:num\b[^>]*>.*?</w:num>`)
	numPicBulletRe  = regexp.MustCompile(`(?s)<w:numPicBullet\b.*?</w:numPicBullet>`)
	numberingRootRe = regexp.MustCompile(`(?s)<w:numbering\b[^>]*>`)
	abstractIDRe    = regexp.MustCompile(`w:abstractNumId="(\d+)"`)
	abstractRefRe   = regexp.MustCompile(`<w:abstractNumId w:val="(\d+)"/>`)
	numIDRe         = regexp.MustCompile(`w:numId="(\d+)"`)
	numRefRe        = regexp.MustCompile(`<w:numId w:val="(\d+)"/>`)
)

func highest(re *regexp.Regexp, text string) int {
	top := -1
	for _, m := range re.FindAllStringSubmatch(text, -1) {
		if n, err := strconv.Atoi(m[1]); err == nil && n > top {
			top = n
		}
	}
	return top
}

// shift adds by to every number a pattern's first group holds.
func shift(re *regexp.Regexp, text string, by int) string {
	if by == 0 {
		return text
	}
	return re.ReplaceAllStringFunc(text, func(found string) string {
		m := re.FindStringSubmatch(found)
		n, _ := strconv.Atoi(m[1])
		return strings.Replace(found, `"`+m[1]+`"`, `"`+strconv.Itoa(n+by)+`"`, 1)
	})
}

// mergeNumbering puts the export's lists after the template's, numbered past them, and answers how far the export's
// list ids moved.
func mergeNumbering(template, ours []byte) ([]byte, int) {
	if len(template) == 0 {
		return ours, 0
	}
	text := string(template)
	absBy := highest(abstractIDRe, text) + 1
	numBy := highest(numIDRe, text) + 1
	mine := shift(abstractIDRe, string(ours), absBy)
	mine = shift(abstractRefRe, mine, absBy)
	mine = shift(numIDRe, mine, numBy)
	root := numberingRootRe.FindString(text)
	if root == "" {
		return ours, 0
	}
	var b strings.Builder
	b.WriteString(text[:strings.Index(text, root)])
	b.WriteString(root)
	for _, re := range []*regexp.Regexp{numPicBulletRe, abstractNumRe} {
		for _, m := range re.FindAllString(text, -1) {
			b.WriteString(m)
		}
	}
	for _, m := range abstractNumRe.FindAllString(mine, -1) {
		b.WriteString(m)
	}
	for _, m := range numRe.FindAllString(text, -1) {
		b.WriteString(m)
	}
	for _, m := range numRe.FindAllString(mine, -1) {
		b.WriteString(m)
	}
	b.WriteString(`</w:numbering>`)
	return []byte(b.String()), numBy
}

var styleRefRe = regexp.MustCompile(`(w:(?:pStyle|rStyle|tblStyle) w:val=")([^"]+)"`)

// restyled writes the export's style ids as the template's own, for the styles Word knows by name.
func restyled(content string, ids map[string]string) string {
	return styleRefRe.ReplaceAllStringFunc(content, func(ref string) string {
		m := styleRefRe.FindStringSubmatch(ref)
		if theirs, ok := ids[m[2]]; ok {
			return m[1] + theirs + `"`
		}
		return ref
	})
}

// pageText is how wide the page's text is: the template's when there is one.
func (w *writer) pageText() int {
	if w.layout != nil {
		return w.layout.width
	}
	return textWidth
}

// framed is word/document.xml written into a template: its body before {{content}}, the export's contents, entries
// and glossary, its body after, and its page.
func (w *writer) framed(content string) []byte {
	ours := styles(w.lang)
	merged, ids := mergeStyles(w.layout.styles, ours)
	w.mergedStyles = merged
	numbering, numBy := mergeNumbering(w.layout.numbering, w.numbering())
	w.mergedNumbering = numbering
	content = shift(numRefRe, restyled(content, ids), numBy)
	var b strings.Builder
	b.WriteString(xml.Header)
	b.WriteString(w.layout.root)
	b.WriteString(`<w:body>`)
	b.WriteString(renamed(fill(w.layout.before, w.fill)))
	b.WriteString(content)
	b.WriteString(renamed(fill(w.layout.after, w.fill)))
	sectPr := w.layout.sectPr
	if sectPr == "" {
		sectPr = defaultSectPr()
	}
	b.WriteString(renamed(sectPr))
	b.WriteString(`</w:body></w:document>`)
	return []byte(b.String())
}

// templateTypes adds to the export's content types those of the template's parts it carries.
func (w *writer) templateTypes(types []byte) []byte {
	text := string(types)
	var add strings.Builder
	for name := range w.layout.parts {
		ext := strings.ToLower(strings.TrimPrefix(path.Ext(name), "."))
		if kind, ok := w.layout.defaults[ext]; ok && !strings.Contains(text+add.String(), `Extension="`+ext+`"`) {
			add.WriteString(`<Default Extension="` + ext + `" ContentType="` + kind + `"/>`)
		}
	}
	for name, kind := range w.layout.overrides {
		add.WriteString(`<Override PartName="/` + name + `" ContentType="` + kind + `"/>`)
	}
	at := strings.LastIndex(text, "</Types>")
	return []byte(text[:at] + add.String() + text[at:])
}

// templateParts are the template's parts, headers and footers filled in.
func (w *writer) templateParts() map[string][]byte {
	out := make(map[string][]byte, len(w.layout.parts))
	for name, data := range w.layout.parts {
		base := path.Base(name)
		if strings.HasSuffix(name, ".xml") && (strings.HasPrefix(base, "header") || strings.HasPrefix(base, "footer")) {
			data = []byte(fill(string(data), w.fill))
		}
		out[name] = data
	}
	return out
}
