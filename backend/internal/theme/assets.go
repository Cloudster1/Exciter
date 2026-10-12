package theme

import (
	"bytes"
	"errors"
	"net/http"
	"regexp"
	"strings"
)

var (
	// ErrBadAssetType is returned for a file a theme cannot use.
	ErrBadAssetType = errors.New("a theme takes PNG, JPEG, WebP, GIF or SVG pictures and WOFF or WOFF2 fonts")
	// ErrUnsafeSVG is returned for an SVG that carries script or reaches out.
	ErrUnsafeSVG = errors.New("that SVG carries script, event handlers or links to elsewhere, which a theme cannot use")
	// ErrAssetTooLarge is returned for a file over the limit.
	ErrAssetTooLarge = errors.New("that file is too large: a theme's files are up to 12 MB each")
	// ErrTooManyAssets is returned at the limit of files per theme.
	ErrTooManyAssets = errors.New("a theme holds at most 64 files; delete one first")
	// ErrAssetInUse is returned when deleting a file the theme still names.
	ErrAssetInUse = errors.New("that file is still used by the theme; take it out of the theme first")
)

// What an SVG must not carry to be drawn as a mask, a cursor or a backdrop.
var unsafeSVG = []*regexp.Regexp{
	regexp.MustCompile(`(?i)<\s*(script|foreignobject|iframe|embed|object|use|animate|set)\b`),
	regexp.MustCompile(`(?i)<[^>]*\son[a-z]+\s*=`),
	regexp.MustCompile(`(?i)javascript:`),
	regexp.MustCompile(`(?i)(href|src)\s*=\s*["']?\s*(https?:|//|data:text)`),
	regexp.MustCompile(`(?i)@import`),
	regexp.MustCompile(`(?i)url\(\s*["']?\s*(https?:|//)`),
}

// sniffHead is how much of a file its kind is read from; svgSearch how far an
// SVG's opening tag may sit behind a declaration and comments.
const (
	sniffHead = 512
	svgSearch = 2048
)

// sniff decides what a file is from its bytes, the name helping only for
// SVG, which is text. The type is what the bytes say, never what was claimed.
func sniff(name string, data []byte) (string, error) {
	if int64(len(data)) > MaxAssetBytes {
		return "", ErrAssetTooLarge
	}
	if len(data) == 0 {
		return "", ErrBadAssetType
	}
	switch {
	case bytes.HasPrefix(data, []byte("wOF2")):
		return "font/woff2", nil
	case bytes.HasPrefix(data, []byte("wOFF")):
		return "font/woff", nil
	}
	detected := http.DetectContentType(data)
	switch detected {
	case "image/png", "image/jpeg", "image/webp", "image/gif":
		return detected, nil
	}
	if looksLikeSVG(name, data) {
		for _, bad := range unsafeSVG {
			if bad.Match(data) {
				return "", ErrUnsafeSVG
			}
		}
		return "image/svg+xml", nil
	}
	return "", ErrBadAssetType
}

func looksLikeSVG(name string, data []byte) bool {
	head := strings.ToLower(string(bytes.TrimLeft(data[:min(len(data), sniffHead)], "\xef\xbb\xbf \t\r\n")))
	if !strings.HasPrefix(head, "<svg") && !strings.HasPrefix(head, "<?xml") {
		return false
	}
	return strings.HasSuffix(strings.ToLower(name), ".svg") || strings.Contains(strings.ToLower(string(data[:min(len(data), svgSearch)])), "<svg")
}
