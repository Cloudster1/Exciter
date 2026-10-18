// Package ziplimit reads the files of an uploaded zip archive against limits of its own, whatever the archive's
// directory claims, so a small upload never unpacks into more than the server allows.
package ziplimit

import (
	"archive/zip"
	"fmt"
	"io"
	"path"
	"strings"
)

// Bundle is an archive's files by path, the one folder they may all sit in left out, read against a total.
type Bundle struct {
	Files    map[string]*zip.File
	Names    []string
	total    int64
	unpacked int64
}

// TooLargeError is a file past its limit; Whole says the archive as a whole went past its total.
type TooLargeError struct {
	Name  string
	Limit int64
	Whole bool
}

func (e *TooLargeError) Error() string {
	if e.Whole {
		return fmt.Sprintf("the archive unpacks into more than %d bytes", e.Limit)
	}
	return fmt.Sprintf("%s unpacks into more than %d bytes", e.Name, e.Limit)
}

// ReadError is a file of the archive that could not be unpacked.
type ReadError struct {
	Name string
	Err  error
}

func (e *ReadError) Error() string { return fmt.Sprintf("read %s: %v", e.Name, e.Err) }

func (e *ReadError) Unwrap() error { return e.Err }

// Open lists an archive's files, everything unpacked from it together held to total bytes.
func Open(zr *zip.Reader, total int64) *Bundle {
	b := &Bundle{Files: map[string]*zip.File{}, total: total}
	names := make([]string, 0, len(zr.File))
	for _, f := range zr.File {
		name := strings.ReplaceAll(f.Name, "\\", "/")
		if strings.HasSuffix(name, "/") || name == "" {
			continue
		}
		names = append(names, name)
	}
	prefix := CommonFolder(names)
	for _, f := range zr.File {
		name := strings.ReplaceAll(f.Name, "\\", "/")
		if strings.HasSuffix(name, "/") || name == "" {
			continue
		}
		name = path.Clean(strings.TrimPrefix(name, prefix))
		if strings.HasPrefix(name, "../") || name == "." || name == ".." {
			continue
		}
		if _, dup := b.Files[name]; dup {
			continue
		}
		b.Files[name] = f
		b.Names = append(b.Names, name)
	}
	return b
}

// CommonFolder is the one folder every file sits in, as an archive of a folder holds it, or nothing.
func CommonFolder(names []string) string {
	if len(names) == 0 {
		return ""
	}
	first, _, found := strings.Cut(names[0], "/")
	if !found {
		return ""
	}
	prefix := first + "/"
	for _, n := range names[1:] {
		if !strings.HasPrefix(n, prefix) {
			return ""
		}
	}
	return prefix
}

// Read unpacks the file at name, refusing more than limit bytes of it.
func (b *Bundle) Read(name string, limit int64) ([]byte, error) {
	f := b.Files[name]
	if f == nil {
		return nil, &ReadError{Name: name, Err: fmt.Errorf("no such file")}
	}
	return b.ReadFile(f, name, limit)
}

// ReadFile unpacks one file, refusing more than limit bytes of it and more than the total of the archive.
func (b *Bundle) ReadFile(f *zip.File, name string, limit int64) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, &ReadError{Name: name, Err: err}
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, limit+1))
	if err != nil {
		return nil, &ReadError{Name: name, Err: err}
	}
	if int64(len(data)) > limit {
		return nil, &TooLargeError{Name: name, Limit: limit}
	}
	b.unpacked += int64(len(data))
	if b.unpacked > b.total {
		return nil, &TooLargeError{Name: name, Limit: b.total, Whole: true}
	}
	return data, nil
}
