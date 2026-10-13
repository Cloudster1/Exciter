package attachment

import (
	"bytes"
	"encoding/base64"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"

	"github.com/cloudster1/exciter/backend/internal/config"
)

func TestTheDefaultLimitIsTheConfigured(t *testing.T) {
	if DefaultMaxSize != config.DefaultUploadLimit {
		t.Fatalf("attachment.DefaultMaxSize %d and config.DefaultUploadLimit %d disagree", DefaultMaxSize, config.DefaultUploadLimit)
	}
}

func TestTooLargeNamesTheLimit(t *testing.T) {
	err := error(&TooLargeError{Limit: 50 * mib})
	if !errors.Is(err, ErrTooLarge) || !strings.Contains(err.Error(), "50 MB") {
		t.Fatalf("got %v", err)
	}
	for n, want := range map[int64]string{mib: "1 MB", 3 * mib / 2: "1.5 MB", 512 * kib: "512 KB", 100: "100 bytes"} {
		if got := Size(n); got != want {
			t.Errorf("Size(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestAnUploadIsHeldToTheLimitAndMustHoldSomething(t *testing.T) {
	const limit = 8
	s := (&Service{MaxSize: DefaultMaxSize}).WithMaxSize(limit)
	if data, err := s.readWhole(strings.NewReader("12345678")); err != nil || len(data) != limit {
		t.Fatalf("a file of exactly the limit was refused: %v", err)
	}
	var tooLarge *TooLargeError
	if _, err := s.readWhole(strings.NewReader("123456789")); !errors.As(err, &tooLarge) || tooLarge.Limit != limit {
		t.Fatalf("a byte over the limit gave %v", err)
	}
	if _, err := s.readWhole(strings.NewReader("")); !errors.Is(err, ErrEmpty) {
		t.Fatalf("an empty file gave %v", err)
	}
	if s.WithMaxSize(0).MaxSize != limit {
		t.Fatal("a limit of nothing replaced the one set")
	}
}

func TestContentTypeTrustsASpecificClaimAndSniffsTheRest(t *testing.T) {
	pngBytes := encoded(t, "png")
	cases := []struct {
		declared string
		data     []byte
		want     string
	}{
		{"application/pdf", []byte("%PDF-1.7"), "application/pdf"},
		{"", pngBytes, "image/png"},
		{"application/octet-stream", pngBytes, "image/png"},
		{"text/html\r\nX-Evil: 1", []byte("hello"), "text/plain; charset=utf-8"},
		{"not a type", []byte("hello"), "text/plain; charset=utf-8"},
	}
	for _, c := range cases {
		if got := contentTypeFor(c.declared, c.data); got != c.want {
			t.Errorf("contentTypeFor(%q) = %q, want %q", c.declared, got, c.want)
		}
	}
}

func TestOnlyWhatCannotRunScriptsShowsInPlace(t *testing.T) {
	for contentType, want := range map[string]bool{
		"image/png": true, "IMAGE/WEBP": true, "application/pdf": true, "text/plain; charset=utf-8": true,
		"image/svg+xml": false, "text/html": false, "application/zip": false, "": false,
	} {
		if got := ShowsInPlace(contentType); got != want {
			t.Errorf("ShowsInPlace(%q) = %v, want %v", contentType, got, want)
		}
	}
}

// webp1x1 is the smallest lossless WebP there is, one pixel.
const webp1x1 = "UklGRhoAAABXRUJQVlA4TA0AAAAvAAAAEAcQERGIiP4HAA=="

func encoded(t *testing.T, kind string) []byte {
	t.Helper()
	img := image.NewPaletted(image.Rect(0, 0, 3, 2), color.Palette{color.Black, color.White})
	var buf bytes.Buffer
	var err error
	switch kind {
	case "png":
		err = png.Encode(&buf, img)
	case "jpeg":
		err = jpeg.Encode(&buf, img, nil)
	case "gif":
		err = gif.Encode(&buf, img, nil)
	}
	if err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestImagesReportTheirSize(t *testing.T) {
	for _, kind := range []string{"png", "jpeg", "gif"} {
		w, h := dimensions("image/"+kind, encoded(t, kind))
		if w == nil || h == nil || *w != 3 || *h != 2 {
			t.Errorf("%s measured %v x %v, want 3 x 2", kind, w, h)
		}
	}
	webp, _ := base64.StdEncoding.DecodeString(webp1x1)
	if w, h := dimensions("image/webp", webp); w == nil || *w != 1 || *h != 1 {
		t.Errorf("webp measured %v x %v, want 1 x 1", w, h)
	}
	if w, h := dimensions("image/svg+xml", []byte(`<svg width="10" height="10"/>`)); w != nil || h != nil {
		t.Error("an SVG was measured")
	}
	if w, _ := dimensions("image/png", []byte("not a png")); w != nil {
		t.Error("broken bytes were measured")
	}
}
