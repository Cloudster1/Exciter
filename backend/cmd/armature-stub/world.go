package main

import (
	"strings"
	"sync"

	"github.com/google/uuid"
)

// The stub's pretend Armature: organizations made on first use from the
// tokens that name them, so every test starts from the same world and none
// sees another's changes.

// namespace makes the stub's ids from names, so they are the same on every run.
var namespace = uuid.MustParse("6d1f8f3e-2b7a-4c4e-9a53-5a8f0c2e7b11")

func idOf(parts ...string) uuid.UUID {
	return uuid.NewSHA1(namespace, []byte(strings.Join(parts, "/")))
}

const (
	// adminName is the person Armature makes an administrator of every tenant.
	adminName = "admin"
	// readerName holds a token of the read scope, refused every write.
	readerName = "reader"
	// revokedToken is always refused, as a token revoked in Armature is.
	revokedToken = "armature_pat_revoked"
	tokenPrefix  = "armature_pat_"
)

type person struct {
	ID       uuid.UUID
	Name     string
	Display  string
	Email    string
	ReadOnly bool
}

type tenant struct {
	Slug   string
	OrgID  uuid.UUID
	people map[string]*person
}

type world struct {
	mu      sync.Mutex
	tenants map[string]*tenant
}

func newWorld() *world { return &world{tenants: map[string]*tenant{}} }

// tenant returns the organization of that slug, making it on first use.
func (w *world) tenant(slug string) *tenant {
	if t, ok := w.tenants[slug]; ok {
		return t
	}
	t := &tenant{Slug: slug, OrgID: idOf(slug, "org"), people: map[string]*person{}}
	w.tenants[slug] = t
	return t
}

// caller reads a token armature_pat_{tenant}_{person}; ok is false for one
// Armature would answer 401.
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
		ID:       idOf(t.Slug, "person", name),
		Name:     name,
		Display:  strings.ToUpper(name[:1]) + name[1:],
		Email:    name + "@" + t.Slug + ".armature.test",
		ReadOnly: name == readerName,
	}
	t.people[name] = p
	return p
}
