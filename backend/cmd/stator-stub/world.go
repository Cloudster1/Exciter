package main

import (
	"strings"
	"sync"

	"github.com/google/uuid"
)

// The stub's pretend Stator, as armature-stub's: tenants made on first use from the tokens that name them, so every
// test starts from the same world and none sees another's people.

// namespace makes the stub's ids from names, so they are the same on every run.
var namespace = uuid.MustParse("3f0b5d2a-8c41-4e7f-b6a2-1d9e4c7a5b30")

func idOf(parts ...string) uuid.UUID {
	return uuid.NewSHA1(namespace, []byte(strings.Join(parts, "/")))
}

const (
	// revokedToken is always refused, as a token revoked in Stator is.
	revokedToken = "stator_pat_revoked"
	tokenPrefix  = "stator_pat_"
)

type person struct {
	ID      uuid.UUID
	Name    string
	Display string
	Email   string
}

type tenant struct {
	Slug   string
	people map[string]*person
}

type world struct {
	mu      sync.Mutex
	tenants map[string]*tenant
	// capabilities is what GET /meta lists; Stator lists none today.
	capabilities []string
}

func newWorld() *world { return &world{tenants: map[string]*tenant{}, capabilities: []string{}} }

// tenant returns the tenant of that slug, making it on first use.
func (w *world) tenant(slug string) *tenant {
	if t, ok := w.tenants[slug]; ok {
		return t
	}
	t := &tenant{Slug: slug, people: map[string]*person{}}
	w.tenants[slug] = t
	return t
}

// caller reads a token stator_pat_{tenant}_{person}; ok is false for one Stator would answer 401.
func (w *world) caller(token string) (*tenant, *person, bool) {
	if token == revokedToken || !strings.HasPrefix(token, tokenPrefix) {
		return nil, nil, false
	}
	slug, name, found := strings.Cut(strings.TrimPrefix(token, tokenPrefix), "_")
	if !found || slug == "" || name == "" {
		return nil, nil, false
	}
	t := w.tenant(slug)
	return t, t.person(name), true
}

func (t *tenant) person(name string) *person {
	if p, ok := t.people[name]; ok {
		return p
	}
	p := &person{
		ID:      idOf(t.Slug, "person", name),
		Name:    name,
		Display: strings.ToUpper(name[:1]) + name[1:],
		Email:   name + "@" + t.Slug + ".stator.test",
	}
	t.people[name] = p
	return p
}
