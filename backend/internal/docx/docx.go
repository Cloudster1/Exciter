// Package docx writes a specification as a Word document (#45), Office Open XML made from its entries' documents
// with their pictures inside. Adapted from Stator's internal/docx, the writing half, cut to a requirement's blocks.
package docx

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/specdoc"
)

// ContentType is what a Word document is served as.
const ContentType = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"

// MaxBytes bounds the pictures one document carries, as a PDF is bounded: a file this large is no longer one to
// mail or open offline.
const MaxBytes int64 = 50 << 20

// ErrTooLarge refuses a specification whose pictures would make the file larger than MaxBytes.
var ErrTooLarge = errors.New("the specification's pictures make the Word file larger than this server hands out")

// Reader reads what the entries' documents point at, as the person exporting may.
type Reader struct {
	// Picture opens a file's bytes; nil and no error is a file the reader may not open.
	Picture func(ctx context.Context, id uuid.UUID) (io.ReadCloser, error)
	// BaseURL is the web client's address, which links within it are read against.
	BaseURL string
}

// Write writes the document as a .docx, its added words in lang, "en" or "de".
func Write(ctx context.Context, out io.Writer, doc *specdoc.Document, lang string, r Reader) error {
	w := newWriter(ctx, lang, r)
	body, err := w.document(doc)
	if err != nil {
		return err
	}
	return w.pack(out, body, doc.ExportedAt, coreProperties(doc, lang), lang)
}

// pack writes the document's parts as the .docx's zip, its pictures beside them.
func (w *writer) pack(out io.Writer, body []byte, modified time.Time, core []byte, lang string) error {
	zw := zip.NewWriter(out)
	part := func(name string, data []byte) error {
		f, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate, Modified: modified})
		if err != nil {
			return err
		}
		_, err = f.Write(data)
		return err
	}
	parts := []struct {
		name string
		data []byte
	}{
		{"[Content_Types].xml", contentTypes(w.media)},
		{"_rels/.rels", []byte(packageRels)},
		{"docProps/core.xml", core},
		{"docProps/app.xml", []byte(appProperties)},
		{"word/document.xml", body},
		{"word/styles.xml", styles(lang)},
		{"word/numbering.xml", w.numbering()},
		{"word/settings.xml", []byte(settings)},
		{"word/_rels/document.xml.rels", w.relationships()},
	}
	for _, m := range w.media {
		parts = append(parts, struct {
			name string
			data []byte
		}{"word/" + m.target, m.data})
	}
	for _, f := range parts {
		if err := part(f.name, f.data); err != nil {
			return fmt.Errorf("write %s: %w", f.name, err)
		}
	}
	return zw.Close()
}

// Bytes is Write into memory, so a failure is known before anything is sent.
func Bytes(ctx context.Context, doc *specdoc.Document, lang string, r Reader) ([]byte, error) {
	var buf bytes.Buffer
	if err := Write(ctx, &buf, doc, lang, r); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

var unslug = regexp.MustCompile(`[^a-z0-9]+`)

// FileName names a document after what it is of and the day, as its PDF is named; ext is pdf or docx.
func FileName(doc *specdoc.Document, ext string) string {
	name := doc.ProjectKey
	switch doc.Kind {
	case specdoc.KindSection:
		name = doc.SectionKey
	case specdoc.KindBaseline:
		if slug := strings.Trim(unslug.ReplaceAllString(strings.ToLower(doc.Title), "-"), "-"); slug != "" {
			name += "-" + slug
		}
	}
	return name + "-" + doc.ExportedAt.Format(time.DateOnly) + "." + ext
}

// language is the document's language as Word names it.
func language(lang string) string {
	if strings.EqualFold(lang, "de") {
		return "de-DE"
	}
	return "en-US"
}
