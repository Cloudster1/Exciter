package ziplimit

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"errors"
	"hash/crc32"
	"slices"
	"strings"
	"testing"
)

// entry is one file of a test archive; claim, when set, is the size its directory says it unpacks into.
type entry struct {
	name  string
	data  []byte
	claim uint64
}

// archive zips the entries as written, keeping a claimed size where one is given whatever the data holds.
func archive(t *testing.T, entries ...entry) *zip.Reader {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		if e.claim == 0 {
			w, err := zw.Create(e.name)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := w.Write(e.data); err != nil {
				t.Fatal(err)
			}
			continue
		}
		var packed bytes.Buffer
		fw, err := flate.NewWriter(&packed, flate.BestCompression)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = fw.Write(e.data)
		_ = fw.Close()
		w, err := zw.CreateRaw(&zip.FileHeader{Name: e.name, Method: zip.Deflate, CRC32: crc32.ChecksumIEEE(e.data),
			CompressedSize64: uint64(packed.Len()), UncompressedSize64: e.claim})
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write(packed.Bytes())
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	return zr
}

// The limits of the tests, small so an archive past them is quick to write.
const (
	fileLimit  = 100
	totalLimit = 250
	pastLimit  = 1000
	small      = 10
)

func filler(n int) []byte { return bytes.Repeat([]byte("a"), n) }

// A file past its limit is refused by its name, whatever its directory claims, and files together past the total
// are refused as the whole archive (#318).
func TestAFileIsReadOnlyWithinItsLimitAndTheArchivesTotal(t *testing.T) {
	t.Run("a file past its limit names itself", func(t *testing.T) {
		b := Open(archive(t, entry{name: "big.reqif", data: filler(pastLimit)}), totalLimit)
		_, err := b.Read("big.reqif", fileLimit)
		var large *TooLargeError
		if !errors.As(err, &large) || large.Whole || large.Name != "big.reqif" || large.Limit != fileLimit {
			t.Fatalf("read a file past its limit: %v", err)
		}
		if !strings.Contains(err.Error(), "big.reqif unpacks into more than 100 bytes") {
			t.Errorf("the refusal says %q", err)
		}
	})

	t.Run("a file that claims a small size and unpacks into more is refused, never read past its claim", func(t *testing.T) {
		b := Open(archive(t, entry{name: "bomb.reqif", data: filler(pastLimit), claim: small}), totalLimit)
		data, err := b.Read("bomb.reqif", fileLimit)
		// The zip reader stops at the claimed size, so the lie is refused as damaged before the limit is reached.
		var damaged *ReadError
		if !errors.As(err, &damaged) || !errors.Is(err, zip.ErrFormat) || data != nil {
			t.Fatalf("read a file that lies about its size: %d bytes, %v", len(data), err)
		}
	})

	t.Run("a file that claims a size past its limit is read no further than the limit", func(t *testing.T) {
		b := Open(archive(t, entry{name: "honest.reqif", data: filler(pastLimit), claim: pastLimit}), totalLimit)
		_, err := b.Read("honest.reqif", fileLimit)
		var large *TooLargeError
		if !errors.As(err, &large) || large.Whole || b.unpacked != 0 {
			t.Fatalf("read a file past its limit: %v, %d bytes counted", err, b.unpacked)
		}
	})

	t.Run("files each within the limit and together past the total are refused as the whole archive", func(t *testing.T) {
		b := Open(archive(t, entry{name: "a.png", data: filler(fileLimit)}, entry{name: "b.png", data: filler(fileLimit)},
			entry{name: "c.png", data: filler(fileLimit)}), totalLimit)
		for _, name := range []string{"a.png", "b.png"} {
			if _, err := b.Read(name, fileLimit); err != nil {
				t.Fatalf("read %s within the total: %v", name, err)
			}
		}
		_, err := b.Read("c.png", fileLimit)
		var large *TooLargeError
		if !errors.As(err, &large) || !large.Whole || large.Limit != totalLimit {
			t.Fatalf("read past the total: %v", err)
		}
		if err.Error() != "the archive unpacks into more than 250 bytes" {
			t.Errorf("the refusal says %q", err)
		}
	})

	t.Run("a file the archive lacks is a read error naming it", func(t *testing.T) {
		b := Open(archive(t, entry{name: "a.reqif", data: filler(small)}), totalLimit)
		var missing *ReadError
		if _, err := b.Read("b.reqif", fileLimit); !errors.As(err, &missing) || missing.Name != "b.reqif" {
			t.Errorf("read a missing file: %v", err)
		}
	})
}

// A path that climbs out of the archive and a second file of the same name are left out, and the one folder every
// file sits in is stripped (#318).
func TestAnArchiveListsOnlyTheFilesItMayHold(t *testing.T) {
	t.Run("a climbing path and a duplicate are left out", func(t *testing.T) {
		b := Open(archive(t,
			entry{name: "spec.reqif", data: []byte("first")},
			entry{name: "../escape.png", data: filler(small)},
			entry{name: "media/../../escape.png", data: filler(small)},
			entry{name: "media/", data: nil},
			entry{name: "spec.reqif", data: []byte("second")},
			entry{name: `media\shot.png`, data: filler(small)},
		), totalLimit)
		if !slices.Equal(b.Names, []string{"spec.reqif", "media/shot.png"}) {
			t.Fatalf("the archive lists %v", b.Names)
		}
		data, err := b.Read("spec.reqif", fileLimit)
		if err != nil || string(data) != "first" {
			t.Errorf("the duplicate replaced the first: %q, %v", data, err)
		}
	})

	t.Run("one common folder is stripped", func(t *testing.T) {
		b := Open(archive(t, entry{name: "export/spec.reqif", data: filler(small)}, entry{name: "export/media/shot.png", data: filler(small)}), totalLimit)
		if !slices.Equal(b.Names, []string{"spec.reqif", "media/shot.png"}) {
			t.Errorf("the archive lists %v", b.Names)
		}
	})

	t.Run("two folders keep their names", func(t *testing.T) {
		b := Open(archive(t, entry{name: "one/spec.reqif", data: filler(small)}, entry{name: "two/shot.png", data: filler(small)}), totalLimit)
		if !slices.Equal(b.Names, []string{"one/spec.reqif", "two/shot.png"}) {
			t.Errorf("the archive lists %v", b.Names)
		}
	})

	for _, c := range []struct {
		what  string
		names []string
		want  string
	}{
		{"nothing", nil, ""},
		{"a file at the top", []string{"spec.reqif"}, ""},
		{"one folder", []string{"export/spec.reqif", "export/media/shot.png"}, "export/"},
		{"a file outside the folder", []string{"export/spec.reqif", "shot.png"}, ""},
		{"a folder whose name starts the same", []string{"export/spec.reqif", "exported/shot.png"}, ""},
	} {
		if got := CommonFolder(c.names); got != c.want {
			t.Errorf("%s: the common folder is %q, want %q", c.what, got, c.want)
		}
	}
}
