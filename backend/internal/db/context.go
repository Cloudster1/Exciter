package db

import (
	"context"
	"strings"

	"github.com/google/uuid"
)

type (
	pinKey  struct{}
	userKey struct{}
)

// UserVar is the setting the permission policies read through
// current_actor_id(): whom the transaction acts for.
const UserVar = "app.user_id"

// TokenProjectsVar is the setting that limits the person to the projects of the
// token they act with, read by the permission functions; unset is no limit.
const TokenProjectsVar = "app.token_projects"

// acting is whom the transactions act for and, for a token limited to some
// projects, which; one value so that naming another person drops the limit.
type acting struct {
	user         uuid.UUID
	projectsOnly bool
	projects     []uuid.UUID
}

// WithUser names the person the transactions made with ctx act for, so row
// level security holds them to that person's permissions.
func WithUser(ctx context.Context, id uuid.UUID) context.Context {
	return context.WithValue(ctx, userKey{}, acting{user: id})
}

// WithUserInProjects is WithUser for a token that reaches only the projects
// named, none when the list is empty.
func WithUserInProjects(ctx context.Context, id uuid.UUID, projects []uuid.UUID) context.Context {
	return context.WithValue(ctx, userKey{}, acting{user: id, projectsOnly: true, projects: projects})
}

// UserFrom is the person WithUser named, if any.
func UserFrom(ctx context.Context) (uuid.UUID, bool) {
	a, ok := ctx.Value(userKey{}).(acting)
	return a.user, ok && a.user != uuid.Nil
}

// ProjectsFrom is the projects WithUserInProjects limited the person to; ok is
// false when nothing limits them.
func ProjectsFrom(ctx context.Context) (projects []uuid.UUID, ok bool) {
	a, _ := ctx.Value(userKey{}).(acting)
	return a.projects, a.projectsOnly
}

// projectsSetting spells the projects as the Postgres array the setting holds.
func projectsSetting(projects []uuid.UUID) string {
	parts := make([]string, len(projects))
	for i, p := range projects {
		parts[i] = p.String()
	}
	return "{" + strings.Join(parts, ",") + "}"
}

// pin describes a freshness requirement placed on reads made with a context.
type pin struct {
	// forcePrimary sends every read to the primary regardless of replica state.
	forcePrimary bool
	// requiredLSN is the WAL position a replica must have replayed before it
	// may serve this context's reads.
	requiredLSN LSN
}

// PinPrimary forces every read made with the returned context to the primary,
// for the rare read that must see the absolute latest state.
func PinPrimary(ctx context.Context) context.Context {
	p, _ := ctx.Value(pinKey{}).(pin)
	p.forcePrimary = true
	return context.WithValue(ctx, pinKey{}, p)
}

// PinLSN requires any replica serving this context to have replayed lsn, so a
// user never reads past their own write; otherwise the read goes to the primary.
func PinLSN(ctx context.Context, lsn LSN) context.Context {
	p, _ := ctx.Value(pinKey{}).(pin)
	if lsn > p.requiredLSN {
		p.requiredLSN = lsn
	}
	return context.WithValue(ctx, pinKey{}, p)
}

func pinFrom(ctx context.Context) pin {
	p, _ := ctx.Value(pinKey{}).(pin)
	return p
}
