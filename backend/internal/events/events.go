// Package events is the transactional outbox, as in Armature: an event is
// committed with the change that caused it, and the worker drains the table.
package events

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"

	"github.com/cloudster1/exciter/backend/internal/db"
)

// Topics are dotted names, what happened to what, such as requirement.revised;
// each feature adds its own here as it lands, and Topics lists them all.
var Topics = []string{"project.key_changed", "project.deleted", "requirement.created", "requirement.revised", "requirement.changed",
	"requirement.trashed", "requirement.restored", "requirement.moved", "link.created", "link.removed", "import.requested", "armature.links",
	"comment.created", "comment.edited", "thread.resolved", "thread.reopened"}

// Event is one committed domain event.
type Event struct {
	ID        uuid.UUID
	Topic     string
	Payload   json.RawMessage
	CreatedAt time.Time
	// Trace is the W3C traceparent of the request that emitted it, empty when
	// none was sampled.
	Trace string
}

// Emit writes an event in the caller's transaction, the one that makes the
// change. The payload's actorId must be the person the transaction acts for.
func Emit(ctx context.Context, tx db.DBTX, topic string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal %s payload: %w", topic, err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO outbox_event (topic, payload, trace_parent)
		VALUES ($1, $2, NULLIF($3, ''))`,
		topic, body, TraceParent(ctx)); err != nil {
		return fmt.Errorf("emit %s: %w", topic, err)
	}
	return nil
}

// TraceParent is the W3C traceparent of ctx, empty when none is sampled, for an
// event written in SQL rather than through Emit.
func TraceParent(ctx context.Context) string {
	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	return carrier["traceparent"]
}

// continueTrace puts the emitting request's trace on ctx, so the work done
// for the event joins it.
func continueTrace(ctx context.Context, e Event) context.Context {
	if e.Trace == "" {
		return ctx
	}
	return otel.GetTextMapPropagator().Extract(ctx, propagation.MapCarrier{"traceparent": e.Trace})
}
