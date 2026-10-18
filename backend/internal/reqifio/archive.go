package reqifio

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"path"
	"strings"

	"github.com/cloudster1/exciter/backend/internal/ziplimit"
)

// readArchive reads a .reqifz: one .reqif document, and the files beside it that its rich text shows.
func readArchive(data []byte) (*File, []Embedded, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, nil, invalid("The archive could not be opened; it is damaged or no zip. Export the .reqifz again and upload the new one.")
	}
	b := openBundle(zr)
	var docs []string
	for _, name := range b.Names {
		if strings.EqualFold(path.Ext(name), ".reqif") {
			docs = append(docs, name)
		}
	}
	switch {
	case len(docs) == 0:
		return nil, nil, invalid("The archive holds no .reqif document. Upload a .reqifz with one ReqIF document in it, or the .reqif file itself.")
	case len(docs) > 1:
		return nil, nil, invalid("The archive holds %d .reqif documents, and an import reads one. Unpack it and import the documents one after the other.", len(docs))
	}
	doc, err := b.read(docs[0], MaxDocumentBytes)
	if err != nil {
		return nil, nil, err
	}
	f, err := readDocument(doc)
	if err != nil {
		return nil, nil, err
	}
	base := path.Dir(docs[0])
	var embedded []Embedded
	seen := map[string]bool{}
	for _, ref := range f.objectRefs() {
		if seen[ref] {
			continue
		}
		seen[ref] = true
		entry := b.Files[path.Clean(path.Join(base, ref))]
		if entry == nil || int64(entry.UncompressedSize64) > MaxImageBytes {
			continue
		}
		bytes, err := b.readFile(entry, ref, MaxImageBytes)
		var large *tooLargeError
		if errors.As(err, &large) && !large.whole {
			continue
		}
		if err != nil {
			return nil, nil, err
		}
		if len(bytes) == 0 {
			continue
		}
		img := Image{Path: ref, ContentType: http.DetectContentType(bytes), Size: len(bytes)}
		f.Images = append(f.Images, img)
		embedded = append(embedded, Embedded{Image: img, Data: bytes})
	}
	return f, embedded, nil
}

// objectRefs are the paths of the files the rich text of the file's objects shows, in the order they are met.
func (f *File) objectRefs() []string {
	var refs []string
	for _, o := range f.Objects {
		for _, v := range o.Values {
			if v.XHTML == nil {
				continue
			}
			v.XHTML.walk(func(n *XNode) bool {
				if n.Name != "object" {
					return true
				}
				if chosen, ok := chooseObject(n); ok {
					refs = append(refs, chosen.Attrs["data"])
				}
				return false
			})
		}
	}
	return refs
}

// walk visits n and what is below it while visit says to go on below.
func (n *XNode) walk(visit func(*XNode) bool) {
	if !visit(n) {
		return
	}
	for i := range n.Children {
		n.Children[i].walk(visit)
	}
}

// chooseObject picks what an object shows: the first image among it and the objects nested in it as its
// fallbacks, else the object itself, as long as it names a file of the archive.
func chooseObject(n *XNode) (*XNode, bool) {
	var image, first *XNode
	n.walk(func(o *XNode) bool {
		if o.Name != "object" || image != nil {
			return image == nil
		}
		ref := strings.TrimSpace(o.Attrs["data"])
		if ref == "" || strings.Contains(ref, ":") || strings.HasPrefix(ref, "/") {
			return true
		}
		if first == nil {
			first = o
		}
		if looksLikeImage(o) {
			image = o
		}
		return true
	})
	if image != nil {
		return image, true
	}
	return first, first != nil
}

// imageTypes are the images a statement shows in place, as attachments measure them.
var imageTypes = map[string]bool{"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true}

var imageExtensions = map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true}

func looksLikeImage(o *XNode) bool {
	return imageTypes[strings.ToLower(strings.TrimSpace(o.Attrs["type"]))] ||
		imageExtensions[strings.ToLower(path.Ext(o.Attrs["data"]))]
}

// bundle is an archive's files by path, the one folder they may all sit in left out, read against
// MaxUnpackedBytes whatever the archive's directory claims.
type bundle struct{ *ziplimit.Bundle }

// tooLargeError is a file of the archive past its limit; whole says the archive as a whole went past its own.
type tooLargeError struct {
	FileError
	whole bool
}

func openBundle(zr *zip.Reader) *bundle { return &bundle{ziplimit.Open(zr, MaxUnpackedBytes)} }

func (b *bundle) read(name string, limit int64) ([]byte, error) {
	return b.readFile(b.Files[name], name, limit)
}

// readFile unpacks one file, refusing more than limit bytes of it.
func (b *bundle) readFile(f *zip.File, name string, limit int64) ([]byte, error) {
	data, err := b.ReadFile(f, name, limit)
	var large *ziplimit.TooLargeError
	switch {
	case errors.As(err, &large) && large.Whole:
		return nil, &tooLargeError{FileError: FileError{fmt.Sprintf("The archive unpacks into more than %d MB, more than one import takes. Export the specifications in several files, and import them one after the other.", MaxUnpackedMB)}, whole: true}
	case errors.As(err, &large):
		return nil, &tooLargeError{FileError: FileError{fmt.Sprintf("The archive's %s unpacks into more than %d MB, more than one import takes. Export the specifications in several files, and import them one after the other.", name, limit/megabyte)}}
	case err != nil:
		return nil, invalid("The archive's %s could not be read. Export the .reqifz again and upload the new one.", name)
	}
	return data, nil
}
