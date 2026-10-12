// Package tenant carries the current organization through a request. It has no
// database dependency, so db can import it to scope row level security.
package tenant

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

// PostgresVar is the setting every row level security policy reads through
// current_org_id(). It is set per transaction, so it cannot leak across a pool.
const PostgresVar = "app.org_id"

// ErrNoTenant is returned when a tenant scoped operation has no organization.
var ErrNoTenant = errors.New("no organization in context")

type ctxKey struct{}

// Org identifies the organization a request is acting within.
type Org struct {
	ID   uuid.UUID
	Slug string
}

// WithOrg returns a context bound to org.
func WithOrg(ctx context.Context, org Org) context.Context {
	return context.WithValue(ctx, ctxKey{}, org)
}

// FromContext returns the organization bound to ctx, if any.
func FromContext(ctx context.Context) (Org, bool) {
	org, ok := ctx.Value(ctxKey{}).(Org)
	return org, ok
}

// MustFromContext returns the organization bound to ctx or ErrNoTenant.
func MustFromContext(ctx context.Context) (Org, error) {
	org, ok := FromContext(ctx)
	if !ok || org.ID == uuid.Nil {
		return Org{}, ErrNoTenant
	}
	return org, nil
}
