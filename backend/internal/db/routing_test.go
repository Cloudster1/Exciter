package db

import (
	"testing"
	"time"
)

// newReplica builds a replica in a given health state.
func newReplica(name string, healthy bool) *replicaState {
	r := &replicaState{name: name}
	r.healthy.Store(healthy)
	return r
}

func TestPickReplica(t *testing.T) {
	tests := []struct {
		name        string
		replicas    []*replicaState
		start       int
		pin         pin
		wantOutcome routeOutcome
		wantReplica string
	}{
		{
			name:        "no replicas configured falls back to primary",
			wantOutcome: routeNoHealthy,
		},
		{
			name:        "explicit primary pin skips healthy replicas",
			replicas:    []*replicaState{newReplica("a", true)},
			pin:         pin{forcePrimary: true},
			wantOutcome: routePinned,
		},
		{
			name:        "healthy replica serves an unpinned read",
			replicas:    []*replicaState{newReplica("a", true)},
			wantOutcome: routeReplica,
			wantReplica: "a",
		},
		{
			name:        "unhealthy replicas are skipped",
			replicas:    []*replicaState{newReplica("a", false), newReplica("b", true)},
			wantOutcome: routeReplica,
			wantReplica: "b",
		},
		{
			name:        "all replicas unhealthy falls back to primary",
			replicas:    []*replicaState{newReplica("a", false), newReplica("b", false)},
			wantOutcome: routeNoHealthy,
		},
		{
			// The health loop's figure is seconds old; only the connection the
			// read gets can say whether it has replayed the caller's write.
			name:        "a required position is left for the connection to judge",
			replicas:    []*replicaState{newReplica("a", true)},
			pin:         pin{requiredLSN: 100},
			wantOutcome: routeReplica,
			wantReplica: "a",
		},
		{
			name:        "a negative rotation start still lands on a replica",
			replicas:    []*replicaState{newReplica("a", true), newReplica("b", true)},
			start:       -3,
			wantOutcome: routeReplica,
			wantReplica: "b",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, outcome := pickReplica(tt.replicas, tt.start, tt.pin)
			if outcome != tt.wantOutcome {
				t.Fatalf("outcome = %v, want %v", outcome, tt.wantOutcome)
			}
			if tt.wantReplica == "" {
				if got != nil {
					t.Fatalf("expected no replica, got %q", got.name)
				}
				return
			}
			if got == nil || got.name != tt.wantReplica {
				t.Fatalf("replica = %v, want %q", got, tt.wantReplica)
			}
		})
	}
}

// TestPickReplicaRotates asserts that consecutive reads spread across replicas
// rather than pinning every read to the same one.
func TestPickReplicaRotates(t *testing.T) {
	replicas := []*replicaState{newReplica("a", true), newReplica("b", true), newReplica("c", true)}
	seen := map[string]int{}
	for i := range 30 {
		r, outcome := pickReplica(replicas, i, pin{})
		if outcome != routeReplica {
			t.Fatalf("read %d was not served by a replica: %v", i, outcome)
		}
		seen[r.name]++
	}
	if len(seen) != 3 {
		t.Fatalf("expected all three replicas to be used, got %v", seen)
	}
	for name, n := range seen {
		if n != 10 {
			t.Errorf("replica %s served %d of 30 reads, want an even 10", name, n)
		}
	}
}

func TestAdmit(t *testing.T) {
	const maxLag = 2 * time.Second
	standby := func(lsn LSN, lag time.Duration) connSample {
		return connSample{inRecovery: true, replayLSN: lsn, lag: lag, receiver: "streaming"}
	}
	tests := []struct {
		name   string
		sample connSample
		pin    pin
		maxLag time.Duration
		want   routeOutcome
	}{
		{"a caught up standby serves an unpinned read", standby(100, 0), pin{}, maxLag, routeReplica},
		{"a standby past the caller's write serves it", standby(150, 0), pin{requiredLSN: 100}, maxLag, routeReplica},
		{"a standby exactly at the caller's write serves it", standby(100, 0), pin{requiredLSN: 100}, maxLag, routeReplica},
		{"a standby short of the caller's write sends it to the primary", standby(99, 0), pin{requiredLSN: 100}, maxLag, routeStale},
		{"a standby short of the write is stale even with no lag", standby(1, 0), pin{requiredLSN: 2}, 0, routeStale},
		{"a standby over the lag bound sends reads to the primary", standby(100, 3*time.Second), pin{}, maxLag, routeLagging},
		{"a standby at the lag bound still serves", standby(100, maxLag), pin{}, maxLag, routeReplica},
		{"no bound admits any lag", standby(100, time.Hour), pin{}, 0, routeReplica},
		{"a standby whose receiver dropped is not trusted", connSample{inRecovery: true, replayLSN: 100, receiver: "waiting"}, pin{}, maxLag, routeLagging},
		{"an unreadable receiver view falls back on the lag figure", connSample{inRecovery: true, replayLSN: 100}, pin{}, maxLag, routeReplica},
		{"a promoted node has everything it saw", connSample{replayLSN: 1}, pin{requiredLSN: 100}, maxLag, routeReplica},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := admit(tt.sample, tt.pin, tt.maxLag); got != tt.want {
				t.Fatalf("admit = %v, want %v", got, tt.want)
			}
		})
	}
}

// Two connections of one pool reach two replicas behind a read service; the
// verdict follows the connection, not the pool.
func TestAdmitJudgesEachConnection(t *testing.T) {
	caller := pin{requiredLSN: 500}
	ahead := connSample{inRecovery: true, replayLSN: 600, receiver: "streaming"}
	behind := connSample{inRecovery: true, replayLSN: 400, receiver: "streaming"}
	if admit(ahead, caller, 0) != routeReplica {
		t.Error("the connection on the replica past the write was refused")
	}
	if admit(behind, caller, 0) != routeStale {
		t.Error("the connection on the replica short of the write was admitted")
	}
}

func TestPinsCombine(t *testing.T) {
	ctx := PinLSN(t.Context(), 200)
	ctx = PinLSN(ctx, 100)
	if got := pinFrom(ctx).requiredLSN; got != 200 {
		t.Fatalf("an older position lowered the requirement to %v", got)
	}
	ctx = PinPrimary(ctx)
	if p := pinFrom(ctx); !p.forcePrimary || p.requiredLSN != 200 {
		t.Fatalf("pinning to the primary lost the position: %+v", p)
	}
}
