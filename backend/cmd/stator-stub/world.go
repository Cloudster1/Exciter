package main

import (
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// The stub's pretend Stator, as armature-stub's: tenants made on first use from the tokens that name them, each with
// the same spaces and pages, so every test starts from the same world and none sees another's changes.

// namespace makes the stub's ids from names, so they are the same on every run.
var namespace = uuid.MustParse("3f0b5d2a-8c41-4e7f-b6a2-1d9e4c7a5b30")

func idOf(parts ...string) uuid.UUID {
	return uuid.NewSHA1(namespace, []byte(strings.Join(parts, "/")))
}

// seeded is when the fixed pages were made.
var seeded = time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)

const (
	// revokedToken is always refused, as a token revoked in Stator is.
	revokedToken = "stator_pat_revoked"
	tokenPrefix  = "stator_pat_"
	// adminName is the person who reads the SEC space; everybody reads ENG.
	adminName = "admin"

	spaceOpen   = "ENG"
	spaceSecret = "SEC"
)

type person struct {
	ID      uuid.UUID
	Name    string
	Display string
	Email   string
}

type space struct {
	ID   uuid.UUID
	Key  string
	Name string
	// AdminOnly spaces are read by adminName alone.
	AdminOnly bool
	Home      *page
}

type page struct {
	ID        uuid.UUID
	Space     *space
	Parent    *page
	Title     string
	Version   int
	Home      bool
	CreatedAt time.Time
	UpdatedAt time.Time
	UpdatedBy string
	// Trashed pages answer to nobody, as Stator's trash does.
	Trashed bool
}

type tenant struct {
	Slug   string
	people map[string]*person
	spaces []*space
	// pages are in the order they were made, which a search keeps.
	pages []*page
	// webhook is the endpoint its changes are posted to, and deliveries what became of the latest.
	webhook    *webhook
	deliveries []delivery
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
	t := seed(slug)
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

func seed(slug string) *tenant {
	t := &tenant{Slug: slug, people: map[string]*person{}}
	for _, spec := range []struct {
		key, name, home string
		adminOnly       bool
	}{{spaceOpen, "Engineering", "Engineering home", false}, {spaceSecret, "Security", "Security home", true}} {
		sp := &space{ID: idOf(slug, "space", spec.key), Key: spec.key, Name: spec.name, AdminOnly: spec.adminOnly}
		t.spaces = append(t.spaces, sp)
		sp.Home = t.add(sp, nil, spec.home, 1)
		sp.Home.Home = true
	}
	eng, sec := t.spaces[0], t.spaces[1]
	braking := t.add(eng, eng.Home, "Braking system", 3)
	t.add(eng, braking, "Brake timing", 2)
	t.add(eng, eng.Home, "Onboarding guide", 1)
	t.add(sec, sec.Home, "Threat model", 1)
	return t
}

func (t *tenant) add(sp *space, parent *page, title string, version int) *page {
	at := seeded.Add(time.Duration(len(t.pages)) * time.Hour)
	p := &page{ID: idOf(t.Slug, "page", sp.Key, title), Space: sp, Parent: parent, Title: title, Version: version,
		CreatedAt: at, UpdatedAt: at, UpdatedBy: "Alice"}
	t.pages = append(t.pages, p)
	return p
}

func (t *tenant) space(key string) *space {
	for _, sp := range t.spaces {
		if sp.Key == strings.ToUpper(key) {
			return sp
		}
	}
	return nil
}

func (p *person) reads(sp *space) bool { return sp != nil && (!sp.AdminOnly || p.Name == adminName) }

// page finds a page out of the trash by its id, whoever asks; readable is the same for a person.
func (t *tenant) page(id uuid.UUID) *page {
	for _, p := range t.pages {
		if p.ID == id && !p.Trashed {
			return p
		}
	}
	return nil
}

func (t *tenant) readable(who *person, id uuid.UUID) *page {
	p := t.page(id)
	if p == nil || !who.reads(p.Space) {
		return nil
	}
	return p
}

// ancestors are the pages above p, the space's home first.
func (p *page) ancestors() []*page {
	var out []*page
	for up := p.Parent; up != nil; up = up.Parent {
		out = append([]*page{up}, out...)
	}
	return out
}

func (p *page) path() []string {
	out := []string{}
	for _, a := range p.ancestors() {
		out = append(out, a.Title)
	}
	return out
}
