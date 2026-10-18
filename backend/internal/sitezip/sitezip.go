// Package sitezip builds a specification's static HTML site (#214) from the print view as the render service hands it
// over: a page per top-level part, links between them, the pictures beside them and a search that needs no server.
package sitezip

import (
	"archive/zip"
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// Agreed limits of a site (#242).
const (
	// MaxSearchText is how much of each requirement's words the search holds.
	MaxSearchText = 2000
	// MaxPictureBytes bounds one picture carried into the site.
	MaxPictureBytes = 50 << 20
)

// ErrNotPrintView is HTML that holds no print view of a specification.
var ErrNotPrintView = errors.New("the page is not a printed specification")

// Picture opens a file a page shows, by its id; nil and no error is one the reader may not open.
type Picture func(ctx context.Context, id uuid.UUID) (io.ReadCloser, error)

// Labels are the words the site adds, in the reader's language.
type Labels struct {
	Contents, Search, NoMatch, Previous, Next, Glossary string
}

var attachmentRe = regexp.MustCompile(`/attachments/([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})`)

// page is one file of the site: its name, title and the nodes it holds.
type page struct {
	file, title string
	nodes       []*html.Node
}

// entry is one requirement as the search finds it.
type entry struct {
	Page  string `json:"page"`
	ID    string `json:"id"`
	Key   string `json:"key"`
	Title string `json:"title"`
	Text  string `json:"text"`
}

func attr(n *html.Node, name string) (string, bool) {
	for _, a := range n.Attr {
		if a.Key == name {
			return a.Val, true
		}
	}
	return "", false
}

func setAttr(n *html.Node, name, value string) {
	for i, a := range n.Attr {
		if a.Key == name {
			n.Attr[i].Val = value
			return
		}
	}
	n.Attr = append(n.Attr, html.Attribute{Key: name, Val: value})
}

func find(n *html.Node, match func(*html.Node) bool) *html.Node {
	if match(n) {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if found := find(c, match); found != nil {
			return found
		}
	}
	return nil
}

func walk(n *html.Node, visit func(*html.Node)) {
	visit(n)
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		walk(c, visit)
	}
}

func hasAttr(name string) func(*html.Node) bool {
	return func(n *html.Node) bool {
		_, ok := attr(n, name)
		return n.Type == html.ElementNode && ok
	}
}

func textOf(n *html.Node) string {
	var b strings.Builder
	walk(n, func(c *html.Node) {
		if c.Type == html.TextNode {
			b.WriteString(c.Data)
			b.WriteByte(' ')
		}
	})
	return strings.Join(strings.Fields(b.String()), " ")
}

// Build cuts a printed specification into a zipped site that opens from a folder.
func Build(ctx context.Context, printed []byte, title string, labels Labels, pictures Picture) ([]byte, error) {
	root, err := html.Parse(bytes.NewReader(printed))
	if err != nil {
		return nil, fmt.Errorf("read the printed page: %w", err)
	}
	article := find(root, hasAttr("data-print-document"))
	if article == nil {
		return nil, ErrNotPrintView
	}
	var css strings.Builder
	walk(root, func(n *html.Node) {
		if n.Type == html.ElementNode && n.DataAtom == atom.Style && n.FirstChild != nil {
			css.WriteString(n.FirstChild.Data)
		}
	})

	// The cover and the contents open the site; each top-level entry starts a page of its own, and the glossary is
	// the last.
	index := &page{file: "index.html", title: title}
	var pages []*page
	var glossary *page
	var current *page
	for c := article.FirstChild; c != nil; {
		next := c.NextSibling
		article.RemoveChild(c)
		switch {
		case hasAttr("data-print-glossary")(c):
			glossary = &page{file: "glossary.html", title: labels.Glossary, nodes: []*html.Node{c}}
		case hasAttr("data-print-entry")(c):
			depth, _ := attr(c, "data-entry-depth")
			if current == nil || depth == "0" {
				current = &page{file: "part-" + strconv.Itoa(len(pages)+1) + ".html", title: textOf(firstHeading(c))}
				pages = append(pages, current)
			}
			current.nodes = append(current.nodes, c)
		default:
			index.nodes = append(index.nodes, c)
		}
		c = next
	}
	if glossary != nil {
		pages = append(pages, glossary)
	}

	// Where each anchor is now, so a link within the specification reaches its page.
	home := map[string]string{"glossary": "glossary.html"}
	var found []entry
	for _, p := range pages {
		for _, n := range p.nodes {
			walk(n, func(c *html.Node) {
				if id, ok := attr(c, "id"); ok && c.Type == html.ElementNode {
					home[id] = p.file
				}
			})
			if key, ok := attr(n, "data-print-entry"); ok {
				text := textOf(n)
				if len(text) > MaxSearchText {
					text = text[:MaxSearchText]
				}
				found = append(found, entry{Page: p.file, ID: "entry-" + key, Key: key, Title: textOf(firstHeading(n)), Text: text})
			}
		}
	}
	files := map[string][]byte{}
	carried := map[string]string{}
	for _, p := range append([]*page{index}, pages...) {
		for _, n := range p.nodes {
			if err := relink(ctx, n, p.file, home, pictures, carried, files); err != nil {
				return nil, err
			}
		}
	}
	files["style.css"] = []byte(css.String())
	search, err := searchScript(found)
	if err != nil {
		return nil, err
	}
	files["search.js"] = search
	files["index.html"] = render(index, labels, nil, nil, true)
	for i, p := range pages {
		var prev, next *page
		if i > 0 {
			prev = pages[i-1]
		}
		if i+1 < len(pages) {
			next = pages[i+1]
		}
		files[p.file] = render(p, labels, prev, next, false)
	}
	return zipped(files)
}

func firstHeading(n *html.Node) *html.Node {
	h := find(n, func(c *html.Node) bool {
		switch c.DataAtom {
		case atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6:
			return true
		}
		return false
	})
	if h == nil {
		return n
	}
	return h
}

// relink points a page's links at the pages their anchors are on, and its pictures at files beside it.
func relink(ctx context.Context, n *html.Node, file string, home map[string]string, pictures Picture, carried map[string]string, files map[string][]byte) error {
	var failed error
	walk(n, func(c *html.Node) {
		if c.Type != html.ElementNode || failed != nil {
			return
		}
		switch c.DataAtom {
		case atom.A:
			href, _ := attr(c, "href")
			if anchor, ok := strings.CutPrefix(href, "#"); ok {
				if target, known := home[anchor]; known && target != file {
					setAttr(c, "href", target+href)
				}
			} else if strings.HasPrefix(href, "/") {
				// A link into the web client needs the server, which a site opened from a folder has not.
				setAttr(c, "href", "")
			}
		case atom.Img:
			src, _ := attr(c, "src")
			m := attachmentRe.FindStringSubmatch(src)
			if m == nil {
				if strings.HasPrefix(src, "/") {
					setAttr(c, "src", "")
				}
				return
			}
			name, err := carry(ctx, uuid.MustParse(m[1]), pictures, carried, files)
			if err != nil {
				failed = err
				return
			}
			setAttr(c, "src", name)
		}
	})
	return failed
}

// carry copies a picture into the site once, named after it and what its bytes are.
func carry(ctx context.Context, id uuid.UUID, pictures Picture, carried map[string]string, files map[string][]byte) (string, error) {
	if name, done := carried[id.String()]; done {
		return name, nil
	}
	carried[id.String()] = ""
	if pictures == nil {
		return "", nil
	}
	body, err := pictures(ctx, id)
	if err != nil || body == nil {
		return "", err
	}
	defer body.Close()
	data, err := io.ReadAll(io.LimitReader(body, MaxPictureBytes+1))
	if err != nil || len(data) > MaxPictureBytes {
		return "", err
	}
	ext := "bin"
	switch http.DetectContentType(data) {
	case "image/png":
		ext = "png"
	case "image/jpeg":
		ext = "jpg"
	case "image/gif":
		ext = "gif"
	case "image/webp":
		ext = "webp"
	}
	name := "media/" + id.String() + "." + ext
	files[name] = data
	carried[id.String()] = name
	return name, nil
}

// searchScript holds every requirement's words as a variable, which a page opened from a folder can read where it
// could not fetch a file.
func searchScript(found []entry) ([]byte, error) {
	if found == nil {
		found = []entry{}
	}
	data, err := json.Marshal(found)
	if err != nil {
		return nil, err
	}
	safe := strings.NewReplacer("</", `<\/`, " ", ` `, " ", ` `).Replace(string(data))
	return []byte("window.EXCITER_SEARCH = " + safe + ";\n"), nil
}

// finder is the search box's own script: it reads the words search.js holds and lists the requirements that match.
const finder = `(function () {
  var box = document.getElementById("site-search"), out = document.getElementById("site-found");
  if (!box || !out) return;
  box.addEventListener("input", function () {
    var q = box.value.trim().toLowerCase(); out.innerHTML = "";
    if (!q) return;
    var hits = (window.EXCITER_SEARCH || []).filter(function (e) { return (e.key + " " + e.title + " " + e.text).toLowerCase().indexOf(q) >= 0; });
    if (!hits.length) { var li = document.createElement("li"); li.textContent = out.getAttribute("data-none"); out.appendChild(li); return; }
    hits.slice(0, 50).forEach(function (e) {
      var li = document.createElement("li"), a = document.createElement("a");
      a.href = e.page + "#" + e.id; a.textContent = e.title; li.appendChild(a); out.appendChild(li);
    });
  });
})();`

// render writes one page of the site: its own head, the way back to the contents, its nodes and the way on.
func render(p *page, labels Labels, prev, next *page, isIndex bool) []byte {
	var b bytes.Buffer
	b.WriteString("<!doctype html>\n<html><head><meta charset=\"utf-8\"><meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">")
	b.WriteString("<title>" + html.EscapeString(p.title) + "</title><link rel=\"stylesheet\" href=\"style.css\"></head>")
	b.WriteString("<body><main class=\"print-view\" style=\"max-width:56rem;margin:0 auto;padding:2rem 1rem\">")
	nav := func() {
		b.WriteString("<nav class=\"site-nav\" style=\"display:flex;gap:1rem;margin:1rem 0\">")
		b.WriteString("<a href=\"index.html\">" + html.EscapeString(labels.Contents) + "</a>")
		if prev != nil {
			b.WriteString("<a href=\"" + prev.file + "\" rel=\"prev\">" + html.EscapeString(labels.Previous+": "+prev.title) + "</a>")
		}
		if next != nil {
			b.WriteString("<a href=\"" + next.file + "\" rel=\"next\">" + html.EscapeString(labels.Next+": "+next.title) + "</a>")
		}
		b.WriteString("</nav>")
	}
	if !isIndex {
		nav()
	}
	if isIndex {
		b.WriteString("<search style=\"display:block;margin:1rem 0\"><label for=\"site-search\">" + html.EscapeString(labels.Search) + "</label> ")
		b.WriteString("<input id=\"site-search\" type=\"search\"><ul id=\"site-found\" data-none=\"" + html.EscapeString(labels.NoMatch) + "\" aria-live=\"polite\"></ul></search>")
	}
	for _, n := range p.nodes {
		_ = html.Render(&b, n)
	}
	if !isIndex {
		nav()
	}
	b.WriteString("</main>")
	if isIndex {
		b.WriteString("<script src=\"search.js\"></script><script>" + finder + "</script>")
	}
	b.WriteString("</body></html>\n")
	return b.Bytes()
}

func zipped(files map[string][]byte) ([]byte, error) {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	// The index first, so a reader listing the zip sees where to start.
	slices.SortFunc(names, func(a, b string) int {
		return cmp.Or(cmp.Compare(boolRank(a != "index.html"), boolRank(b != "index.html")), cmp.Compare(a, b))
	})
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	now := time.Now()
	for _, name := range names {
		f, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate, Modified: now})
		if err != nil {
			return nil, err
		}
		if _, err := f.Write(files[name]); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func boolRank(b bool) int {
	if b {
		return 1
	}
	return 0
}
