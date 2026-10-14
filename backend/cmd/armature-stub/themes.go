package main

import (
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/theme"
)

// The theme Armature shows each person (#30), Stator's: one of the example themes, or Armature's built-in one.

// maxThemeDelay bounds how long a test may hold back a person's active theme.
const maxThemeDelay = 10 * time.Second

func themeID(t *tenant, p *person, key string) uuid.UUID { return idOf(t.Slug, "theme", p.Name, key) }

func themeView(t *tenant, p *person, e *theme.Example) map[string]any {
	return map[string]any{
		"id": themeID(t, p, e.Key), "ownerId": p.ID, "ownerName": p.Display, "name": e.Name, "shared": false,
		"spec": e.Spec, "assets": []any{}, "inUse": 1, "active": true, "default": false,
		"createdAt": seeded, "updatedAt": p.ThemeChanged,
	}
}

func (s *stub) activeTheme(c *call) {
	e := theme.ExampleByKey(c.person.Theme)
	if e == nil {
		respond(c.w, http.StatusOK, map[string]any{"theme": nil, "source": ""})
		return
	}
	respond(c.w, http.StatusOK, map[string]any{"theme": themeView(c.tenant, c.person, e), "source": "chosen"})
}

func (s *stub) exportTheme(c *call) {
	e := theme.ExampleByKey(c.person.Theme)
	if e == nil || c.r.PathValue("themeID") != themeID(c.tenant, c.person, e.Key).String() {
		refuse(c.w, http.StatusNotFound, "not_found", "That theme was not found.")
		return
	}
	spec := e.Spec
	if c.person.ThemeBroken {
		light := map[string]string{}
		for name := range spec.Colors.Light {
			light[name] = "not a colour"
		}
		spec.Colors.Light = light
	}
	pkg := theme.Package{Format: theme.PackageFormat, Name: e.Name, Spec: spec, Assets: []theme.PackagedAsset{}}
	c.w.Header().Set("Content-Disposition", `attachment; filename="`+e.Key+`.armature-theme.json"`)
	respond(c.w, http.StatusOK, pkg)
}

func (s *stub) setTheme(c *call) {
	var req struct {
		// Theme is the key of an example theme, or empty for Armature's built-in one.
		Theme string `json:"theme"`
		// Broken exports it with colours no theme may hold, as a theme from a later Armature might.
		Broken bool `json:"broken"`
	}
	if !decode(c, &req) {
		return
	}
	if req.Theme != "" && theme.ExampleByKey(req.Theme) == nil {
		refuseField(c.w, "theme", "Name one of the example themes, or leave it empty.")
		return
	}
	p := c.tenant.person(c.r.PathValue("person"))
	p.Theme, p.ThemeBroken, p.ThemeChanged = req.Theme, req.Broken, time.Now().UTC()
	c.w.WriteHeader(http.StatusNoContent)
}

func (s *stub) setThemeDelay(c *call) {
	var req struct {
		// MS holds back every answer to the person's GET /themes/active.
		MS int `json:"ms"`
	}
	if !decode(c, &req) {
		return
	}
	if req.MS < 0 || time.Duration(req.MS)*time.Millisecond > maxThemeDelay {
		refuseField(c.w, "ms", "Give a delay of 0 to 10000 milliseconds.")
		return
	}
	c.tenant.person(c.r.PathValue("person")).ThemeDelay = time.Duration(req.MS) * time.Millisecond
	c.w.WriteHeader(http.StatusNoContent)
}

// themeDelay is how long the caller's GET /themes/active is held back, waited out without the world's lock.
func (s *stub) themeDelay(r *http.Request) time.Duration {
	token, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	s.world.mu.Lock()
	defer s.world.mu.Unlock()
	if _, p, ok := s.world.caller(strings.TrimSpace(token)); ok {
		return p.ThemeDelay
	}
	return 0
}
