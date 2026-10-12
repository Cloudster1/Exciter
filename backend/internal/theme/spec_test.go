package theme

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

func ptr[T any](v T) *T { return &v }

func TestAThemeIsValidatedPartByPart(t *testing.T) {
	themeID := uuid.MustParse("00000000-0000-0000-0000-000000000010")
	file := uuid.MustParse("00000000-0000-0000-0000-000000000020")
	stranger := uuid.MustParse("00000000-0000-0000-0000-000000000030")
	assets := map[uuid.UUID]bool{file: true}

	good := Spec{
		Colors:   Palette{Light: map[string]string{"accent": "#ff0066", "canvas": "rgb(10 20 30)"}, Dark: map[string]string{"accent": "oklch(70% 0.2 250)"}},
		Fonts:    Fonts{Sans: &Font{Family: "Atkinson Hyperlegible", AssetID: &file}},
		Shape:    Shape{RadiusControl: ptr(4), RadiusOverlay: ptr(12)},
		Shadows:  map[string]string{"1": "0 1px 2px rgb(0 0 0 / 0.2)"},
		Cursors:  map[string]Cursor{"pointer": {AssetID: file, HotspotX: 2, HotspotY: 2}},
		Icons:    map[string]Icon{"home": {Paths: []string{"M2 8 8 2l6 6", "M4 7v7h8V7"}}, "search": {AssetID: &file}},
		Backdrop: &Backdrop{AssetID: file, Fit: "cover"},
		CSS:      "[data-rail] { opacity: .9 } .x { background: url(/api/v1/themes/" + themeID.String() + "/assets/" + file.String() + ") }",
	}
	if err := Validate(&good, themeID, assets); err != nil {
		t.Fatalf("a good theme was refused: %v", err)
	}

	refused := []struct {
		what string
		spec Spec
		says string
	}{
		{"a token name with capitals", Spec{Colors: Palette{Light: map[string]string{"Accent": "#fff"}}}, "token name"},
		{"a colour that is a word", Spec{Colors: Palette{Light: map[string]string{"accent": "red"}}}, "not a colour"},
		{"a colour that is an expression", Spec{Colors: Palette{Dark: map[string]string{"accent": "url(x)"}}}, "not a colour"},
		{"a font family with punctuation", Spec{Fonts: Fonts{Sans: &Font{Family: "Comic; Sans"}}}, "family name"},
		{"a font file that is not the theme's", Spec{Fonts: Fonts{Mono: &Font{Family: "Mono", AssetID: &stranger}}}, "not this theme's"},
		{"a radius off the scale", Spec{Shape: Shape{RadiusControl: ptr(99)}}, "radius"},
		{"a fourth shadow", Spec{Shadows: map[string]string{"4": "0 0 1px #000"}}, "shadow is 1, 2 or 3"},
		{"a shadow that loads", Spec{Shadows: map[string]string{"1": "url(http://x)"}}, "box-shadow"},
		{"a cursor nobody has", Spec{Cursors: map[string]Cursor{"crosshair": {AssetID: file}}}, "not a cursor"},
		{"a cursor with a foreign file", Spec{Cursors: map[string]Cursor{"pointer": {AssetID: stranger}}}, "not this theme's"},
		{"a cursor with its hotspot off the picture", Spec{Cursors: map[string]Cursor{"pointer": {AssetID: file, HotspotX: 500}}}, "hotspot"},
		{"an icon with nothing in it", Spec{Icons: map[string]Icon{"home": {}}}, "needs a file or path data"},
		{"an icon with a script for path data", Spec{Icons: map[string]Icon{"home": {Paths: []string{"<script>"}}}}, "path data"},
		{"an icon name with digits", Spec{Icons: map[string]Icon{"icon1": {Paths: []string{"M0 0"}}}}, "icon name"},
		{"a backdrop fitted oddly", Spec{Backdrop: &Backdrop{AssetID: file, Fit: "stretch"}}, "cover or tile"},
		{"css that imports", Spec{CSS: "@import url(http://evil)"}, "@import"},
		{"css that loads from elsewhere", Spec{CSS: ".x { background: url(https://evil/pixel.png) }"}, "only load its own files"},
		{"css that loads another theme's file", Spec{CSS: ".x { background: url(/api/v1/themes/" + stranger.String() + "/assets/" + file.String() + ") }"}, "only load its own files"},
		{"css that closes the style element", Spec{CSS: "</style><script>"}, "</"},
		{"css beyond the limit", Spec{CSS: strings.Repeat("a", MaxCSSBytes+1)}, "at most"},
	}
	for _, each := range refused {
		spec := each.spec
		err := Validate(&spec, themeID, assets)
		if err == nil {
			t.Errorf("%s was accepted", each.what)
		} else if !strings.Contains(err.Error(), each.says) {
			t.Errorf("%s: error %q does not say %q", each.what, err, each.says)
		}
	}

	t.Run("inline images are allowed in css", func(t *testing.T) {
		spec := Spec{CSS: ".x { background: url(\"data:image/svg+xml,%3Csvg%3E\") }"}
		if err := Validate(&spec, themeID, assets); err != nil {
			t.Errorf("an inline image was refused: %v", err)
		}
	})

	t.Run("an empty theme is a theme, with every map present", func(t *testing.T) {
		spec := Spec{}
		if err := Validate(&spec, uuid.Nil, nil); err != nil {
			t.Fatalf("empty refused: %v", err)
		}
		if spec.Colors.Light == nil || spec.Colors.Dark == nil || spec.Cursors == nil || spec.Icons == nil || spec.Shadows == nil {
			t.Error("a map was left nil")
		}
	})

	t.Run("every file the theme names is listed", func(t *testing.T) {
		ids := good.AssetIDs()
		if len(ids) != 5 {
			t.Errorf("AssetIDs = %v, want the font, the cursor, the icon, the backdrop and the css url", ids)
		}
	})
}

func TestFilesAreKnownByTheirBytes(t *testing.T) {
	cases := []struct {
		name string
		data []byte
		want string
		err  error
	}{
		{"face.png", []byte("\x89PNG\r\n\x1a\n" + strings.Repeat("\x00", 16)), "image/png", nil},
		{"font.woff2", []byte("wOF2" + strings.Repeat("\x00", 16)), "font/woff2", nil},
		{"font.woff", []byte("wOFF" + strings.Repeat("\x00", 16)), "font/woff", nil},
		{"glyph.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16"><path d="M2 2h12"/></svg>`), "image/svg+xml", nil},
		{"declared.svg", []byte(`<?xml version="1.0"?><svg><rect/></svg>`), "image/svg+xml", nil},
		{"scripted.svg", []byte(`<svg><script>alert(1)</script></svg>`), "", ErrUnsafeSVG},
		{"handler.svg", []byte(`<svg onload="alert(1)"><rect/></svg>`), "", ErrUnsafeSVG},
		{"reaching.svg", []byte(`<svg><image href="https://evil/x.png"/></svg>`), "", ErrUnsafeSVG},
		{"notes.txt", []byte("just some words"), "", ErrBadAssetType},
		{"empty.png", []byte{}, "", ErrBadAssetType},
		{"huge.png", append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, MaxAssetBytes)...), "", ErrAssetTooLarge},
	}
	for _, each := range cases {
		got, err := sniff(each.name, each.data)
		if err != each.err {
			t.Errorf("%s: err = %v, want %v", each.name, err, each.err)
		}
		if got != each.want {
			t.Errorf("%s: type = %q, want %q", each.name, got, each.want)
		}
	}
}
