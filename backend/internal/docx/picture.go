package docx

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"io"
	"strconv"

	"github.com/google/uuid"
	_ "golang.org/x/image/webp"

	"github.com/cloudster1/exciter/backend/internal/document"
)

// picture is a file made ready to embed: its relationship and its size in pixels.
type picture struct {
	rid           string
	width, height int
}

// embed adds a file to the document once, PNG, JPEG and GIF as they are and WebP
// as PNG; nil is a file the reader may not open or no picture Word shows.
func (w *writer) embed(id string) *picture {
	if p, seen := w.embedded[id]; seen {
		return p
	}
	p := w.load(id)
	w.embedded[id] = p
	return p
}

func (w *writer) load(id string) *picture {
	parsed, err := uuid.Parse(id)
	if err != nil || w.read.Picture == nil || w.err != nil {
		return nil
	}
	rc, err := w.read.Picture(w.ctx, parsed)
	if err != nil {
		w.fail(err)
		return nil
	}
	if rc == nil {
		return nil
	}
	defer rc.Close()
	left := MaxBytes - w.spent
	data, err := io.ReadAll(io.LimitReader(rc, left+1))
	if err != nil {
		w.fail(fmt.Errorf("read the picture %s: %w", id, err))
		return nil
	}
	return w.place(data)
}

// place adds a picture's bytes to the document within what is left of MaxBytes; nil is no picture Word shows.
func (w *writer) place(data []byte) *picture {
	left := MaxBytes - w.spent
	if int64(len(data)) > left {
		w.fail(ErrTooLarge)
		return nil
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 {
		return nil
	}
	ext := format
	switch format {
	case "png", "gif":
	case "jpeg":
		ext = "jpeg"
	case "webp":
		img, _, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			return nil
		}
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			return nil
		}
		data, ext = buf.Bytes(), "png"
		if int64(len(data)) > left {
			w.fail(ErrTooLarge)
			return nil
		}
	default:
		return nil
	}
	w.spent += int64(len(data))
	target := "media/picture" + strconv.Itoa(len(w.media)+1) + "." + ext
	rid := w.rel(relImage, target, false)
	w.media = append(w.media, media{rid: rid, target: target, ext: ext, data: data})
	return &picture{rid: rid, width: cfg.Width, height: cfg.Height}
}

// drawing writes a picture inline, widthPx wide or as wide as it is, never
// wider than maxTwips, its description for whoever cannot see it.
func (w *writer) drawing(p *picture, widthPx, maxTwips int, descr string) {
	if widthPx <= 0 {
		widthPx = p.width
	}
	cx := int64(widthPx) * emuPerPixel
	if limit := int64(maxTwips) * emuPerTwip; cx > limit {
		cx = limit
	}
	cy := cx * int64(p.height) / int64(p.width)
	w.pictures++
	n := strconv.Itoa(w.pictures)
	ext := `cx="` + strconv.FormatInt(cx, 10) + `" cy="` + strconv.FormatInt(cy, 10) + `"`
	w.b.WriteString(`<w:r><w:drawing><wp:inline distT="0" distB="0" distL="0" distR="0"><wp:extent ` + ext + `/>`)
	w.b.WriteString(`<wp:docPr id="` + n + `" name="Picture ` + n + `" descr="` + escape(descr) + `"/>`)
	w.b.WriteString(`<wp:cNvGraphicFramePr><a:graphicFrameLocks noChangeAspect="1"/></wp:cNvGraphicFramePr>`)
	w.b.WriteString(`<a:graphic><a:graphicData uri="` + nsPic + `"><pic:pic><pic:nvPicPr><pic:cNvPr id="` + n + `" name="Picture ` + n + `" descr="` + escape(descr) + `"/><pic:cNvPicPr/></pic:nvPicPr>`)
	w.b.WriteString(`<pic:blipFill><a:blip r:embed="` + p.rid + `"/><a:stretch><a:fillRect/></a:stretch></pic:blipFill>`)
	w.b.WriteString(`<pic:spPr><a:xfrm><a:off x="0" y="0"/><a:ext ` + ext + `/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></pic:spPr></pic:pic></a:graphicData></a:graphic></wp:inline></w:drawing></w:r>`)
}

func (w *writer) imageBlock(n document.Node, f frame, _ int) {
	alt := stringAttr(n, "alt")
	p := w.embed(stringAttr(n, "attachmentId"))
	if p == nil {
		w.note(f, w.words.pictureMissing(alt))
		return
	}
	w.para(f, paraProps{}, func() { w.drawing(p, intAttr(n, "width", 0), f.width, alt) })
}

// diagramBlock embeds a diagram as the renderer draws it, as PNG since Word shows no SVG it can be sure of; one that
// cannot be drawn is written as its source, with a note saying so.
func (w *writer) diagramBlock(n document.Node, f frame, _ int) {
	language, source := stringAttr(n, "language"), stringAttr(n, "source")
	key := "diagram\x00" + language + "\x00" + source
	p, seen := w.embedded[key]
	if !seen {
		p = w.drawDiagram(language, source)
		w.embedded[key] = p
	}
	if p == nil {
		w.code(source, f)
		if w.err == nil {
			w.note(f, w.words.diagramUndrawn())
		}
		return
	}
	w.para(f, paraProps{}, func() { w.drawing(p, 0, f.width, w.words.diagramDescription(document.DiagramTitle(source))) })
}

func (w *writer) drawDiagram(language, source string) *picture {
	if w.read.Diagram == nil || w.err != nil {
		return nil
	}
	data, err := w.read.Diagram(w.ctx, language, source)
	if err != nil {
		if w.ctx.Err() != nil {
			w.fail(w.ctx.Err())
		}
		return nil
	}
	return w.place(data)
}
