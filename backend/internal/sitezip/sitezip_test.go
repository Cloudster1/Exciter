package sitezip

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/google/uuid"
)

const picture = "0195f000-0000-7000-8000-000000000777"

// printed is a print view as the render service hands it over: a cover, contents, two parts and a glossary.
const printed = `<!doctype html><html><head><style>.print-view{color:black}</style></head><body><main class="print-view" data-print-view="">
<article data-print-document="project">
<section data-print-cover=""><h1>Brakes</h1></section>
<nav data-print-contents=""><ol><li><a href="#entry-CHK-R1">1 Braking</a></li><li><a href="#entry-CHK-R3">2 Lights</a></li><li><a href="#glossary">Glossary</a></li></ol></nav>
<section data-print-entry="CHK-R1" data-entry-depth="0"><h2 id="entry-CHK-R1">1 Braking</h2></section>
<section data-print-entry="CHK-R2" data-entry-depth="1"><h3 id="entry-CHK-R2">1.1 Pedal</h3><p>The pedal shall travel 40 mm, see <a href="#entry-CHK-R3">CHK-R3</a> and <a href="#term-x">brake</a>.</p>
<img src="/api/v1/attachments/` + picture + `/file?inline=1" alt="pedal"><a href="/p/CHK/r/CHK-R2">open</a></section>
<section data-print-entry="CHK-R3" data-entry-depth="0"><h2 id="entry-CHK-R3">2 Lights</h2><p>Lights follow the pedal &lt;/script&gt;.</p></section>
<section data-print-glossary=""><h2 id="glossary">Glossary</h2><dl><dt id="term-x">Brake</dt></dl></section>
</article></main></body></html>`

func unzipped(t *testing.T, data []byte) map[string]string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, f := range zr.File {
		r, _ := f.Open()
		b, _ := io.ReadAll(r)
		out[f.Name] = string(b)
	}
	return out
}

func TestAPrintedSpecificationBecomesLinkedPagesWithASearch(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\n0000")
	pictures := func(_ context.Context, id uuid.UUID) (io.ReadCloser, error) {
		if id.String() != picture {
			t.Errorf("asked for %s", id)
		}
		return io.NopCloser(bytes.NewReader(png)), nil
	}
	data, err := Build(context.Background(), []byte(printed), "Brakes", Labels{Contents: "Contents", Search: "Search", NoMatch: "Nothing", Previous: "Back", Next: "On", Glossary: "Glossary"}, pictures)
	if err != nil {
		t.Fatal(err)
	}
	files := unzipped(t, data)
	for _, name := range []string{"index.html", "part-1.html", "part-2.html", "glossary.html", "search.js", "style.css", "media/" + picture + ".png"} {
		if _, ok := files[name]; !ok {
			t.Errorf("the site lacks %s (has %v)", name, len(files))
		}
	}
	index := files["index.html"]
	if !strings.Contains(index, `href="part-1.html#entry-CHK-R1"`) || !strings.Contains(index, `href="glossary.html#glossary"`) || !strings.Contains(index, `id="site-search"`) {
		t.Errorf("the index reads %s", index)
	}
	part := files["part-1.html"]
	if !strings.Contains(part, "1.1 Pedal") || !strings.Contains(part, `href="part-2.html#entry-CHK-R3"`) || !strings.Contains(part, `href="glossary.html#term-x"`) ||
		!strings.Contains(part, `src="media/`+picture+`.png"`) || strings.Contains(part, "/p/CHK/r/") || !strings.Contains(part, `rel="next"`) {
		t.Errorf("the first part reads %s", part)
	}
	if strings.Contains(files["part-2.html"], "1.1 Pedal") {
		t.Error("the second part holds the first part's requirement")
	}
	search := files["search.js"]
	if !strings.HasPrefix(search, "window.EXCITER_SEARCH = [") || strings.Contains(search, "</script>") || !strings.Contains(search, `"page":"part-1.html"`) {
		t.Errorf("the search reads %s", search)
	}
	if _, err := Build(context.Background(), []byte("<html><body>nothing</body></html>"), "x", Labels{}, nil); err != ErrNotPrintView {
		t.Errorf("a page that is no print view: %v", err)
	}
}
