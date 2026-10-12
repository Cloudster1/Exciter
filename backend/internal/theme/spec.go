// Package theme owns custom themes, a person's redefinition of the tokens and
// glyphs. Adapted from Armature, so either product's theme files import here.
package theme

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

// Limits a theme keeps to, so one document cannot become a payload.
const (
	MaxSpecBytes    = 256 * 1024
	MaxCSSBytes     = 32 * 1024
	MaxTokensPerSet = 64
	MaxIcons        = 128
	MaxPathsPerIcon = 16
	MaxPathBytes    = 2048
	MaxRadius       = 32
	MaxHotspot      = 128
	MaxShadowBytes  = 200
	MaxFamilyBytes  = 64
	// A backdrop may be a moving picture, and an animated one weighs more
	// than a still.
	MaxAssetBytes    = 12 * 1024 * 1024
	MaxAssetsPerSpec = 64
)

// Spec is what a theme says, every part optional. Only overrides are kept:
// what the theme does not name stays as the stylesheet drew it.
type Spec struct {
	Colors   Palette           `json:"colors"`
	Fonts    Fonts             `json:"fonts"`
	Shape    Shape             `json:"shape"`
	Shadows  map[string]string `json:"shadows"`
	Cursors  map[string]Cursor `json:"cursors"`
	Icons    map[string]Icon   `json:"icons"`
	Backdrop *Backdrop         `json:"backdrop,omitempty"`
	// Effect names a moving picture the shell draws live behind the content,
	// which no stylesheet can: nothing in it repeats.
	Effect string `json:"effect,omitempty"`
	CSS    string `json:"css"`
}

// Palette is a set of colour tokens for each of the two built-in themes.
type Palette struct {
	Light map[string]string `json:"light"`
	Dark  map[string]string `json:"dark"`
}

// Fonts names the two faces; an uploaded file makes a face of its own.
type Fonts struct {
	Sans *Font `json:"sans,omitempty"`
	Mono *Font `json:"mono,omitempty"`
}

// Font is a family name and, when uploaded, the file behind it.
type Font struct {
	Family  string     `json:"family"`
	AssetID *uuid.UUID `json:"assetId,omitempty"`
}

// Shape is the two radii.
type Shape struct {
	RadiusControl *int `json:"radiusControl,omitempty"`
	RadiusOverlay *int `json:"radiusOverlay,omitempty"`
}

// Cursor is an uploaded image and where the point is inside it.
type Cursor struct {
	AssetID  uuid.UUID `json:"assetId"`
	HotspotX int       `json:"hotspotX"`
	HotspotY int       `json:"hotspotY"`
}

// Icon replaces one of the kit's glyphs: an uploaded image, or path data
// drawn on the same 16px grid.
type Icon struct {
	AssetID *uuid.UUID `json:"assetId,omitempty"`
	Paths   []string   `json:"paths,omitempty"`
}

// Backdrop is a picture behind the content.
type Backdrop struct {
	AssetID uuid.UUID `json:"assetId"`
	Fit     string    `json:"fit"`
}

// CursorKinds are the pointers a theme may replace, by the name CSS knows.
var CursorKinds = []string{"default", "pointer", "text", "grab", "grabbing", "move", "notAllowed", "wait"}

// ShadowKeys are the three elevations.
var ShadowKeys = []string{"1", "2", "3"}

// BackdropFits are the two ways a backdrop is laid behind the content.
var BackdropFits = []string{"cover", "tile"}

// Effects are the moving pictures the shell knows how to draw.
var Effects = []string{"constellation", "confetti"}

var (
	tokenName  = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	iconName   = regexp.MustCompile(`^[a-z][a-z-]*$`)
	hexColour  = regexp.MustCompile(`^#([0-9a-fA-F]{3}|[0-9a-fA-F]{4}|[0-9a-fA-F]{6}|[0-9a-fA-F]{8})$`)
	funcColour = regexp.MustCompile(`^(rgb|rgba|hsl|hsla|oklch|oklab|color)\([0-9a-z.,%\s/-]+\)$`)
	family     = regexp.MustCompile(`^[A-Za-z0-9 _-]+$`)
	pathData   = regexp.MustCompile(`^[MmLlHhVvCcSsQqTtAaZz0-9 .,eE-]+$`)
	shadowText = regexp.MustCompile(`^[0-9a-zA-Z#.,%()/ -]+$`)
	// A url() reaches the theme's own files or carries the image inline;
	// nothing else, so a theme cannot phone home.
	urlCall = regexp.MustCompile(`(?i)url\(\s*["']?([^"')]*)["']?\s*\)`)
	ownFile = regexp.MustCompile(`^/api/v1/themes/([0-9a-f-]{36})/assets/([0-9a-f-]{36})$`)
)

// forbiddenCSS is what no stylesheet of ours needs and a hostile one would.
var forbiddenCSS = []string{"@import", "expression(", "-moz-binding", "</", "behavior:", "@charset"}

// Validate refuses what the client would not compile or a browser would abuse.
// The theme id and its assets say which url() targets are the theme's own.
func Validate(spec *Spec, themeID uuid.UUID, assets map[uuid.UUID]bool) error {
	if spec == nil {
		return errors.New("a theme needs a body")
	}
	spec.normalise()
	if encoded, err := json.Marshal(spec); err != nil {
		return err
	} else if len(encoded) > MaxSpecBytes {
		return fmt.Errorf("a theme is at most %d KB", MaxSpecBytes/1024)
	}
	own := func(id *uuid.UUID, what string) error {
		if id == nil {
			return nil
		}
		if !assets[*id] {
			return fmt.Errorf("the %s names a file that is not this theme's", what)
		}
		return nil
	}
	for which, set := range map[string]map[string]string{"light": spec.Colors.Light, "dark": spec.Colors.Dark} {
		if len(set) > MaxTokensPerSet {
			return fmt.Errorf("the %s colours name more than %d tokens", which, MaxTokensPerSet)
		}
		for name, value := range set {
			if !tokenName.MatchString(name) {
				return fmt.Errorf("%q is not a colour token name", name)
			}
			if !validColour(value) {
				return fmt.Errorf("%q is not a colour: write #rrggbb, rgb(), hsl() or oklch()", value)
			}
		}
	}
	for which, font := range map[string]*Font{"sans": spec.Fonts.Sans, "mono": spec.Fonts.Mono} {
		if font == nil {
			continue
		}
		if font.Family == "" || len(font.Family) > MaxFamilyBytes || !family.MatchString(font.Family) {
			return fmt.Errorf("the %s font needs a family name of letters, digits, spaces and dashes", which)
		}
		if err := own(font.AssetID, which+" font"); err != nil {
			return err
		}
	}
	for which, radius := range map[string]*int{"control": spec.Shape.RadiusControl, "overlay": spec.Shape.RadiusOverlay} {
		if radius != nil && (*radius < 0 || *radius > MaxRadius) {
			return fmt.Errorf("the %s radius is 0 to %d pixels", which, MaxRadius)
		}
	}
	for key, value := range spec.Shadows {
		if !contains(ShadowKeys, key) {
			return fmt.Errorf("a shadow is 1, 2 or 3, not %q", key)
		}
		if len(value) > MaxShadowBytes || !shadowText.MatchString(value) || strings.Contains(strings.ToLower(value), "url(") {
			return fmt.Errorf("shadow %s is not a box-shadow value", key)
		}
	}
	for kind, cursor := range spec.Cursors {
		if !contains(CursorKinds, kind) {
			return fmt.Errorf("%q is not a cursor a theme can replace", kind)
		}
		if !assets[cursor.AssetID] {
			return fmt.Errorf("the %s cursor names a file that is not this theme's", kind)
		}
		if cursor.HotspotX < 0 || cursor.HotspotY < 0 || cursor.HotspotX > MaxHotspot || cursor.HotspotY > MaxHotspot {
			return fmt.Errorf("a cursor's hotspot is 0 to %d pixels", MaxHotspot)
		}
	}
	if len(spec.Icons) > MaxIcons {
		return fmt.Errorf("a theme replaces at most %d icons", MaxIcons)
	}
	for name, icon := range spec.Icons {
		if !iconName.MatchString(name) {
			return fmt.Errorf("%q is not an icon name", name)
		}
		if icon.AssetID == nil && len(icon.Paths) == 0 {
			return fmt.Errorf("the %s icon needs a file or path data", name)
		}
		if err := own(icon.AssetID, name+" icon"); err != nil {
			return err
		}
		if len(icon.Paths) > MaxPathsPerIcon {
			return fmt.Errorf("the %s icon has more than %d paths", name, MaxPathsPerIcon)
		}
		for _, d := range icon.Paths {
			if d == "" || len(d) > MaxPathBytes || !pathData.MatchString(d) {
				return fmt.Errorf("the %s icon's path data is not SVG path data", name)
			}
		}
	}
	if spec.Backdrop != nil {
		if !assets[spec.Backdrop.AssetID] {
			return errors.New("the backdrop names a file that is not this theme's")
		}
		if !contains(BackdropFits, spec.Backdrop.Fit) {
			return errors.New("a backdrop is fitted as cover or tile")
		}
	}
	if spec.Effect != "" && !contains(Effects, spec.Effect) {
		return fmt.Errorf("%q is not a moving picture the shell can draw", spec.Effect)
	}
	return validateCSS(spec.CSS, themeID, assets)
}

// validateCSS lets any rule through except one that loads from elsewhere.
func validateCSS(css string, themeID uuid.UUID, assets map[uuid.UUID]bool) error {
	if len(css) > MaxCSSBytes {
		return fmt.Errorf("the extra CSS is at most %d KB", MaxCSSBytes/1024)
	}
	lower := strings.ToLower(css)
	for _, word := range forbiddenCSS {
		if strings.Contains(lower, word) {
			return fmt.Errorf("the extra CSS cannot contain %s", word)
		}
	}
	for _, m := range urlCall.FindAllStringSubmatch(css, -1) {
		target := strings.TrimSpace(m[1])
		if strings.HasPrefix(strings.ToLower(target), "data:image/") {
			continue
		}
		if parts := ownFile.FindStringSubmatch(target); parts != nil {
			id, err := uuid.Parse(parts[2])
			if err == nil && parts[1] == themeID.String() && assets[id] {
				continue
			}
		}
		return fmt.Errorf("the extra CSS loads %q; a theme may only load its own files or inline images", target)
	}
	return nil
}

// normalise gives every map a value, so a stored theme reads the same
// whether a part was left out or sent empty.
func (s *Spec) normalise() {
	if s.Colors.Light == nil {
		s.Colors.Light = map[string]string{}
	}
	if s.Colors.Dark == nil {
		s.Colors.Dark = map[string]string{}
	}
	if s.Shadows == nil {
		s.Shadows = map[string]string{}
	}
	if s.Cursors == nil {
		s.Cursors = map[string]Cursor{}
	}
	if s.Icons == nil {
		s.Icons = map[string]Icon{}
	}
	for name, icon := range s.Icons {
		if icon.Paths == nil {
			icon.Paths = []string{}
			s.Icons[name] = icon
		}
	}
	s.CSS = strings.TrimSpace(s.CSS)
}

// AssetIDs lists every file the spec names, so nothing named can be deleted.
func (s *Spec) AssetIDs() []uuid.UUID {
	var out []uuid.UUID
	for _, font := range []*Font{s.Fonts.Sans, s.Fonts.Mono} {
		if font != nil && font.AssetID != nil {
			out = append(out, *font.AssetID)
		}
	}
	for _, cursor := range s.Cursors {
		out = append(out, cursor.AssetID)
	}
	for _, icon := range s.Icons {
		if icon.AssetID != nil {
			out = append(out, *icon.AssetID)
		}
	}
	if s.Backdrop != nil {
		out = append(out, s.Backdrop.AssetID)
	}
	for _, m := range urlCall.FindAllStringSubmatch(s.CSS, -1) {
		if parts := ownFile.FindStringSubmatch(strings.TrimSpace(m[1])); parts != nil {
			if id, err := uuid.Parse(parts[2]); err == nil {
				out = append(out, id)
			}
		}
	}
	return out
}

func validColour(value string) bool {
	value = strings.TrimSpace(value)
	if value == "transparent" {
		return true
	}
	return hexColour.MatchString(value) || funcColour.MatchString(strings.ToLower(value))
}

func contains(list []string, s string) bool {
	for _, each := range list {
		if each == s {
			return true
		}
	}
	return false
}
