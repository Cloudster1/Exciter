package events

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestEachFailedAttemptWaitsLongerThanTheLast(t *testing.T) {
	prev := time.Duration(0)
	for attempts := 1; attempts <= MaxAttempts; attempts++ {
		d := retryDelay(attempts)
		if d <= prev {
			t.Fatalf("attempt %d waits %s, no longer than the %s before it", attempts, d, prev)
		}
		prev = d
	}
	if retryDelay(0) != retryStep {
		t.Errorf("an event that never failed would wait %s, want one step", retryDelay(0))
	}
	// The last retry has to come long before done events are pruned, or its record goes first.
	if last := retryDelay(MaxAttempts); last >= Retention {
		t.Errorf("the last retry waits %s, past how long done events are kept", last)
	}
}

func TestTheMuxRoutesByTopicAndFallsBack(t *testing.T) {
	var got []string
	record := func(name string) Handler {
		return HandlerFunc(func(context.Context, Event) error { got = append(got, name); return nil })
	}
	m := NewMux(record("fallback")).Route("requirement.revised", record("revised"))
	for _, topic := range []string{"requirement.revised", "link.suspect"} {
		if err := m.Handle(t.Context(), Event{Topic: topic}); err != nil {
			t.Fatal(err)
		}
	}
	if len(got) != 2 || got[0] != "revised" || got[1] != "fallback" {
		t.Errorf("handled by %v, want revised then fallback", got)
	}
	if err := NewMux(nil).Handle(t.Context(), Event{Topic: "anything"}); err != nil {
		t.Errorf("a mux with no fallback refused an unrouted topic: %v", err)
	}
}

func TestBeforeRunsAheadOfTheTopicsHandler(t *testing.T) {
	var got []string
	record := func(name string) Handler {
		return HandlerFunc(func(context.Context, Event) error { got = append(got, name); return nil })
	}
	m := NewMux(nil).Route("link.created", record("fan out")).Before("link.created", record("forget")).Before("link.cleared", record("forget"))
	for _, topic := range []string{"link.created", "link.cleared"} {
		if err := m.Handle(t.Context(), Event{Topic: topic}); err != nil {
			t.Fatal(err)
		}
	}
	if strings.Join(got, ", ") != "forget, fan out, forget" {
		t.Errorf("handled by %v, want forget and fan out, then forget alone", got)
	}
}

func TestAChainStopsAtTheFirstFailure(t *testing.T) {
	ran := 0
	boom := errors.New("boom")
	c := Chain{
		HandlerFunc(func(context.Context, Event) error { ran++; return nil }),
		nil,
		HandlerFunc(func(context.Context, Event) error { ran++; return boom }),
		HandlerFunc(func(context.Context, Event) error { ran++; return nil }),
	}
	if err := c.Handle(t.Context(), Event{}); !errors.Is(err, boom) || ran != 2 {
		t.Errorf("the chain returned %v after %d handlers, want boom after 2", err, ran)
	}
}
