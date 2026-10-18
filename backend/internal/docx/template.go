package docx

import (
	"archive/zip"
	"bytes"
	"fmt"
	"html"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/cloudster1/exciter/backend/internal/ziplimit"
)

// A Word template an export is written into (#214): its body around a paragraph that says {{content}}, its styles,
// headers, footers and page, with placeholders filled. The export keeps writing its own structure inside.

// TemplateError refuses a file that cannot frame an export, saying what is wrong as a sentence.
type TemplateError struct{ Message string }

func (e *TemplateError) Error() string { return e.Message }

// Placeholders a template may carry; ContentPlaceholder stands alone in the paragraph the export goes into.
const ContentPlaceholder = "content"

// KnownPlaceholders are the words in double braces an export fills in.
var KnownPlaceholders = []string{"project", "title", "section", "baseline", "date", "exportedBy", "signers", ContentPlaceholder}

// Template is a Word template read and checked, ready to frame exports.
type Template struct {
	root         string
	before       string
	after        string
	sectPr       string
	styles       []byte
	numbering    []byte
	rels         []relationship
	parts        map[string][]byte
	overrides    map[string]string
	defaults     map[string]string
	width        int
	Placeholders []string
}

var (
	placeholderRe = regexp.MustCompile(`\{\{\s*([A-Za-z]+)\s*\}\}`)
	paragraphRe   = regexp.MustCompile(`(?s)<w:p\b[^>]*?(?:/>|>.*?</w:p>)`)
	textRe        = regexp.MustCompile(`(?s)(<w:t(?:\s[^>]*)?>)([^<]*)(</w:t>)`)
	bodyRe        = regexp.MustCompile(`(?s)<w:body>(.*)</w:body>`)
	rootRe        = regexp.MustCompile(`(?s)<w:document\b[^>]*>`)
	sectPrRe      = regexp.MustCompile(`(?s)<w:sectPr\b(?:[^>]*/>|.*</w:sectPr>)\s*$`)
	relIDRe       = regexp.MustCompile(`(r:(?:id|embed|link|pict|href|dm|lo|qs|cs))="([^"]+)"`)
	pgSzRe        = regexp.MustCompile(`<w:pgSz\b[^>]*\bw:w="(\d+)"`)
	pgMarLeftRe   = regexp.MustCompile(`<w:pgMar\b[^>]*\bw:left="(\d+)"`)
	pgMarRightRe  = regexp.MustCompile(`<w:pgMar\b[^>]*\bw:right="(\d+)"`)
)

// wordsOf is the words a piece of document XML shows, its runs joined, as Word may split a word across runs.
func wordsOf(xmlPart string) string {
	var b strings.Builder
	for _, m := range textRe.FindAllStringSubmatch(xmlPart, -1) {
		b.WriteString(html.UnescapeString(m[2]))
	}
	return b.String()
}

// depthBefore counts how many elements named tag are open at the end of a piece of XML.
func depthBefore(xmlPart, tag string) int {
	open := regexp.MustCompile(`<`+tag+`\b[^>]*[^/]>|<`+tag+`>`).FindAllStringIndex(xmlPart, -1)
	closed := strings.Count(xmlPart, "</"+tag+">")
	return len(open) - closed
}

// ReadTemplate checks a .docx or .dotx as a template and reads what an export takes of it.
func ReadTemplate(data []byte) (*Template, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, ErrNotWord
	}
	p := &pkg{b: ziplimit.Open(zr, MaxUnpackedBytes)}
	if p.b.Files["[Content_Types].xml"] == nil {
		return nil, ErrNotWord
	}
	if err := p.readContentTypes(); err != nil {
		return nil, err
	}
	for name, kind := range p.overrides {
		if strings.HasSuffix(strings.ToLower(name), "vbaproject.bin") || strings.Contains(strings.ToLower(kind), "macroenabled") {
			return nil, &TemplateError{"This template holds macros, which Exciter does not run. Save it as a .docx or .dotx without macros."}
		}
	}
	for _, name := range p.b.Names {
		if strings.HasSuffix(strings.ToLower(name), "vbaproject.bin") {
			return nil, &TemplateError{"This template holds macros, which Exciter does not run. Save it as a .docx or .dotx without macros."}
		}
	}
	main := p.mainPart()
	if main == "" {
		return nil, ErrNotWord
	}
	raw, err := p.part(main)
	if err != nil {
		return nil, err
	}
	doc := string(raw)
	t := &Template{parts: map[string][]byte{}, overrides: map[string]string{}, defaults: p.defaults, width: textWidth}
	if t.root = rootRe.FindString(doc); t.root == "" {
		return nil, ErrNotWord
	}
	t.root = declared(t.root)
	m := bodyRe.FindStringSubmatch(doc)
	if m == nil {
		return nil, ErrNotWord
	}
	body := m[1]
	if loc := sectPrRe.FindStringIndex(body); loc != nil {
		t.sectPr, body = body[loc[0]:loc[1]], body[:loc[0]]
	}
	if err := t.split(body); err != nil {
		return nil, err
	}
	if err := t.readParts(p, main); err != nil {
		return nil, err
	}
	t.measure()
	return t, nil
}

// declared adds to the template's root the namespaces the export's pictures and links write, where it lacks them.
func declared(root string) string {
	for prefix, uri := range map[string]string{"w": nsW, "r": nsR, "wp": nsWP, "a": nsA, "pic": nsPic} {
		if !strings.Contains(root, `xmlns:`+prefix+`=`) {
			root = strings.TrimSuffix(root, ">") + ` xmlns:` + prefix + `="` + uri + `">`
		}
	}
	return root
}

// split finds the one paragraph that says {{content}}, and checks every placeholder is one an export fills.
func (t *Template) split(body string) error {
	at := -1
	var end int
	for _, loc := range paragraphRe.FindAllStringIndex(body, -1) {
		if strings.TrimSpace(wordsOf(body[loc[0]:loc[1]])) != "{{"+ContentPlaceholder+"}}" {
			continue
		}
		if at >= 0 {
			return &TemplateError{"Put {{content}} in one paragraph only, where the requirements go."}
		}
		at, end = loc[0], loc[1]
	}
	if at < 0 {
		if strings.Contains(wordsOf(body), "{{"+ContentPlaceholder+"}}") {
			return &TemplateError{"Put {{content}} on a paragraph of its own, with no other words beside it."}
		}
		return &TemplateError{"Put {{content}} on a paragraph of its own where the requirements go."}
	}
	before := body[:at]
	for _, tag := range []string{"w:tbl", "w:sdt", "w:txbxContent"} {
		if depthBefore(before, tag) > 0 {
			return &TemplateError{"Move {{content}} out of the table, text box or content control, onto a paragraph of its own."}
		}
	}
	t.before, t.after = before, body[end:]
	return nil
}

// readParts takes the template's styles, numbering, and every part its body, headers and footers point at.
func (t *Template) readParts(p *pkg, main string) error {
	var err error
	if t.styles, err = p.part("word/styles.xml"); err != nil {
		return err
	}
	if t.numbering, err = p.part("word/numbering.xml"); err != nil {
		return err
	}
	rels, err := p.relationships(main)
	if err != nil {
		return err
	}
	ids := make([]string, 0, len(rels))
	for id := range rels {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	dir := path.Dir(main)
	for _, id := range ids {
		r := rels[id]
		kind := path.Base(r.kind)
		// The export brings its own styles, numbering and settings, merging the template's into them.
		if kind == "styles" || kind == "numbering" || kind == "settings" || kind == "stylesWithEffects" || kind == "glossaryDocument" || kind == "customXml" {
			continue
		}
		target := r.target
		if !r.external {
			if err := t.copyPart(p, target); err != nil {
				return err
			}
			target = strings.TrimPrefix(target, dir+"/")
		}
		t.rels = append(t.rels, relationship{id: "rTpl" + id, kind: r.kind, target: target, external: r.external})
	}
	var used []string
	for _, xmlPart := range append([]string{t.before, t.after}, t.headerFooters()...) {
		for _, m := range placeholderRe.FindAllStringSubmatch(wordsOf(xmlPart), -1) {
			used = append(used, m[1])
		}
	}
	var unknown []string
	for _, name := range used {
		if !slices.Contains(KnownPlaceholders, name) && !slices.Contains(unknown, "{{"+name+"}}") {
			unknown = append(unknown, "{{"+name+"}}")
		}
	}
	if len(unknown) > 0 {
		names := make([]string, 0, len(KnownPlaceholders))
		for _, k := range KnownPlaceholders {
			names = append(names, "{{"+k+"}}")
		}
		return &TemplateError{fmt.Sprintf("%s is not a placeholder. Use %s.", strings.Join(unknown, ", "), strings.Join(names, ", "))}
	}
	t.Placeholders = []string{ContentPlaceholder}
	for _, name := range used {
		if !slices.Contains(t.Placeholders, name) {
			t.Placeholders = append(t.Placeholders, name)
		}
	}
	return nil
}

// copyPart copies a part the template points at, with the parts its own relationships name.
func (t *Template) copyPart(p *pkg, name string) error {
	if _, done := t.parts[name]; done || p.b.Files[name] == nil {
		return nil
	}
	data, err := p.part(name)
	if err != nil {
		return err
	}
	t.parts[name] = data
	if kind := p.overrides[name]; kind != "" {
		t.overrides[name] = kind
	}
	relsName := path.Dir(name) + "/_rels/" + path.Base(name) + ".rels"
	if p.b.Files[relsName] == nil {
		return nil
	}
	if t.parts[relsName], err = p.part(relsName); err != nil {
		return err
	}
	rels, err := p.relationships(name)
	if err != nil {
		return err
	}
	for _, r := range rels {
		if !r.external {
			if err := t.copyPart(p, r.target); err != nil {
				return err
			}
		}
	}
	return nil
}

// headerFooters are the template's header and footer parts, which carry placeholders too.
func (t *Template) headerFooters() []string {
	var out []string
	for name, data := range t.parts {
		base := path.Base(name)
		if strings.HasPrefix(base, "header") || strings.HasPrefix(base, "footer") {
			out = append(out, string(data))
		}
	}
	return out
}

// measure reads how wide the template's text is, so tables and pictures fit its page.
func (t *Template) measure() {
	w, left, right := 0, 0, 0
	if m := pgSzRe.FindStringSubmatch(t.sectPr); m != nil {
		w, _ = strconv.Atoi(m[1])
	}
	if m := pgMarLeftRe.FindStringSubmatch(t.sectPr); m != nil {
		left, _ = strconv.Atoi(m[1])
	}
	if m := pgMarRightRe.FindStringSubmatch(t.sectPr); m != nil {
		right, _ = strconv.Atoi(m[1])
	}
	if w > left+right && w-left-right > margin {
		t.width = w - left - right
	}
}

// Fill is what each placeholder of a template says for one export.
type Fill map[string]string

// fill writes the values into every paragraph whose words carry a placeholder, its words then in its first run.
func fill(xmlPart string, values Fill) string {
	return paragraphRe.ReplaceAllStringFunc(xmlPart, func(par string) string {
		words := wordsOf(par)
		if !placeholderRe.MatchString(words) {
			return par
		}
		filled := placeholderRe.ReplaceAllStringFunc(words, func(ph string) string {
			return values[placeholderRe.FindStringSubmatch(ph)[1]]
		})
		first := true
		return textRe.ReplaceAllStringFunc(par, func(run string) string {
			m := textRe.FindStringSubmatch(run)
			if !first {
				return m[1] + m[3]
			}
			first = false
			return `<w:t xml:space="preserve">` + escape(filled) + m[3]
		})
	})
}

// renamed points the template's relationship ids at the names the export gives them.
func renamed(xmlPart string) string {
	return relIDRe.ReplaceAllString(xmlPart, `$1="rTpl$2"`)
}
