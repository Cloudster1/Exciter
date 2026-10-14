package main

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// The stub's pretend Armature, Stator's: organizations made on first use from the
// tokens that name them, each with the same projects, types and issues, so
// every test starts from the same world and none sees another's changes.

// namespace makes the stub's ids from names, so they are the same on every run.
var namespace = uuid.MustParse("6d1f8f3e-2b7a-4c4e-9a53-5a8f0c2e7b11")

func idOf(parts ...string) uuid.UUID {
	return uuid.NewSHA1(namespace, []byte(strings.Join(parts, "/")))
}

// seeded is when the fixed issues were made and last changed.
var seeded = time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)

const (
	// adminName is the person who sees the SEC project; everybody sees CP.
	adminName = "admin"
	// readerName holds a token of the read scope, refused every write.
	readerName = "reader"
	// revokedToken is always refused, as a token revoked in Armature is.
	revokedToken = "armature_pat_revoked"
	tokenPrefix  = "armature_pat_"

	projectOpen   = "CP"
	projectSecret = "SEC"
)

type person struct {
	ID       uuid.UUID
	Name     string
	Display  string
	Email    string
	ReadOnly bool
	// Reads are the projects they see but may not file issues in.
	Reads map[string]bool
}

func (p *person) writes(pr *project) bool { return p.sees(pr) && !p.Reads[pr.Key] }

type project struct {
	ID   uuid.UUID
	Key  string
	Name string
	// AdminOnly projects are seen by adminName alone.
	AdminOnly bool
}

type issueType struct {
	ID        uuid.UUID
	Name      string
	Icon      string
	Level     int
	IsSubtask bool
	Position  int
}

type status struct {
	ID       uuid.UUID
	Name     string
	Category string
	Position int
}

type issue struct {
	ID       uuid.UUID
	Key      string
	Project  *project
	Number   int
	Type     *issueType
	Summary  string
	Status   *status
	Priority string
	Assignee *person
	Reporter *person
	// Description is the rich text it was filed with, nil for the seeded ones.
	Description json.RawMessage
	// DueDate is a day at midnight UTC, as Armature keeps it.
	DueDate   *time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
	// ResolvedAt is when it was last done, nil while it is not.
	ResolvedAt *time.Time
}

type remoteLink struct {
	ID        uuid.UUID
	IssueID   uuid.UUID
	URL       string
	Title     string
	Source    string
	IconURL   *string
	CreatedBy uuid.UUID
	CreatedAt time.Time
	UpdatedAt time.Time
}

type tenant struct {
	Slug     string
	OrgID    uuid.UUID
	people   map[string]*person
	projects []*project
	types    []*issueType
	statuses []*status
	issues   map[string]*issue
	// moved finds an issue by a key it had before it moved to another project.
	moved map[string]string
	next  map[string]int
	links []*remoteLink
	// outage is how many remote link calls still answer outageStatus, so a
	// test can watch a sync be tried again.
	outage       int
	outageStatus int
	// refused is a summary Armature refuses to file, so a test can watch a create stop part way.
	refused string
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
	t := seed(slug)
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

func seed(slug string) *tenant {
	t := &tenant{
		Slug: slug, OrgID: idOf(slug, "org"), people: map[string]*person{},
		issues: map[string]*issue{}, moved: map[string]string{}, next: map[string]int{},
	}
	t.projects = []*project{
		{ID: idOf(slug, "project", projectOpen), Key: projectOpen, Name: "Core platform"},
		{ID: idOf(slug, "project", projectSecret), Key: projectSecret, Name: "Security", AdminOnly: true},
	}
	for i, spec := range []struct {
		name, icon string
		subtask    bool
		level      int
	}{{"Task", "task", false, 0}, {"Bug", "bug", false, 0}, {"Story", "story", false, 0}, {"Sub-task", "subtask", true, -1}, {"Epic", "epic", false, 1}} {
		t.types = append(t.types, &issueType{ID: idOf(slug, "type", spec.name), Name: spec.name, Icon: spec.icon, Level: spec.level, IsSubtask: spec.subtask, Position: i})
	}
	for i, spec := range [][2]string{{"To do", "todo"}, {"In progress", "in_progress"}, {"Done", "done"}} {
		t.statuses = append(t.statuses, &status{ID: idOf(slug, "status", spec[1]), Name: spec[0], Category: spec[1], Position: i})
	}
	due := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	for i, spec := range []struct {
		project, summary, kind, category, priority, assignee, reporter string
		due                                                            *time.Time
	}{
		{projectOpen, "Set up the build pipeline", "Task", "done", "medium", "alice", "bob", nil},
		{projectOpen, "Sign-in fails with an expired session", "Bug", "in_progress", "high", "bob", "alice", nil},
		{projectOpen, "Write the onboarding guide", "Story", "todo", "low", "", "alice", nil},
		{projectOpen, "Rotate the signing keys", "Task", "todo", "highest", "alice", "bob", &due},
		{projectSecret, "Patch the disclosed vulnerability", "Bug", "in_progress", "highest", adminName, adminName, nil},
		{projectSecret, "Audit the token scopes", "Task", "todo", "medium", "", adminName, nil},
	} {
		at := seeded.Add(time.Duration(i) * time.Hour)
		is := t.add(t.project(spec.project), t.typeNamed(spec.kind), spec.summary, t.person(spec.reporter), at)
		is.Status, is.Priority, is.DueDate, is.UpdatedAt = t.statusOf(spec.category), spec.priority, spec.due, at
		if spec.category == "done" {
			resolved := at.Add(24 * time.Hour)
			is.ResolvedAt, is.UpdatedAt = &resolved, resolved
		}
		if spec.assignee != "" {
			is.Assignee = t.person(spec.assignee)
		}
	}
	// An issue that moved keeps answering to its old key: SEC-2 is CP-5 now.
	t.move(t.issues["SEC-2"], t.project(projectOpen), seeded.Add(6*time.Hour))
	return t
}

func (t *tenant) add(p *project, kind *issueType, summary string, reporter *person, at time.Time) *issue {
	t.next[p.Key]++
	n := t.next[p.Key]
	key := p.Key + "-" + strconv.Itoa(n)
	is := &issue{
		ID: idOf(t.Slug, "issue", key), Key: key, Project: p, Number: n, Type: kind, Summary: summary,
		Status: t.statuses[0], Priority: "medium", Reporter: reporter, CreatedAt: at, UpdatedAt: at,
	}
	t.issues[key] = is
	return is
}

func (t *tenant) move(is *issue, to *project, at time.Time) {
	old := is.Key
	t.next[to.Key]++
	is.Project, is.Number = to, t.next[to.Key]
	is.Key = to.Key + "-" + strconv.Itoa(is.Number)
	is.UpdatedAt = at
	delete(t.issues, old)
	t.issues[is.Key] = is
	t.moved[old] = is.Key
	for from, to := range t.moved {
		if to == old {
			t.moved[from] = is.Key
		}
	}
}

func (t *tenant) project(key string) *project {
	for _, p := range t.projects {
		if p.Key == strings.ToUpper(key) {
			return p
		}
	}
	return nil
}

func (t *tenant) typeNamed(name string) *issueType {
	for _, k := range t.types {
		if k.Name == name {
			return k
		}
	}
	return nil
}

func (t *tenant) typeByID(id uuid.UUID) *issueType {
	for _, k := range t.types {
		if k.ID == id {
			return k
		}
	}
	return nil
}

func (t *tenant) statusOf(category string) *status {
	for _, s := range t.statuses {
		if s.Category == category {
			return s
		}
	}
	return nil
}

func (p *person) sees(pr *project) bool { return pr != nil && (!pr.AdminOnly || p.Name == adminName) }

// lookup finds an issue the person may see by its key or a key it had.
func (t *tenant) lookup(p *person, key string) *issue {
	key = strings.ToUpper(key)
	is, ok := t.issues[key]
	if !ok {
		if now, moved := t.moved[key]; moved {
			is = t.issues[now]
		}
	}
	if is == nil || !p.sees(is.Project) {
		return nil
	}
	return is
}

// visible lists the issues the person may see, by key.
func (t *tenant) visible(p *person) []*issue {
	var out []*issue
	for _, is := range t.issues {
		if p.sees(is.Project) {
			out = append(out, is)
		}
	}
	sort.Slice(out, func(i, j int) bool { return keyLess(out[i], out[j]) })
	return out
}

func keyLess(a, b *issue) bool {
	if a.Project.Key != b.Project.Key {
		return a.Project.Key < b.Project.Key
	}
	return a.Number < b.Number
}
