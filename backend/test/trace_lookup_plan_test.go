//go:build integration

package test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/trace"
)

// endsPerLink is how many requirement rows reading a link with its ends may touch: one lookup by id for each end.
const endsPerLink = 2

// linkLookups records the statements that read a live link with its ends, with the arguments they ran with.
type linkLookups struct {
	mu   sync.Mutex
	seen []pgx.TraceQueryStartData
}

func (l *linkLookups) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if sql := strings.TrimSpace(data.SQL); strings.HasPrefix(sql, "SELECT") && strings.Contains(sql, "FROM trace_link l") {
		l.mu.Lock()
		l.seen = append(l.seen, data)
		l.mu.Unlock()
	}
	return ctx
}

func (*linkLookups) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

// planNode is the part of an EXPLAIN (ANALYZE, FORMAT JSON) node that says how many rows a scan read.
type planNode struct {
	Relation string     `json:"Relation Name"`
	Rows     float64    `json:"Actual Rows"`
	Loops    float64    `json:"Actual Loops"`
	Removed  float64    `json:"Rows Removed by Filter"`
	Plans    []planNode `json:"Plans"`
}

// rowsRead is every row the plan's scans of relation read, kept or filtered out.
func (n planNode) rowsRead(relation string) float64 {
	var sum float64
	if n.Relation == relation {
		sum = (n.Rows + n.Removed) * n.Loops
	}
	for _, c := range n.Plans {
		sum += c.rowsRead(relation)
	}
	return sum
}

// Removing a link reads each of its ends by id, also where the planner would rather not loop: joined freely, a
// hub's links once made it walk every requirement of the installation, checking each, past the timeout (#446).
func TestRemovingALinkReadsOnlyItsEnds(t *testing.T) {
	key := projectKey(t, "ULP")
	f := newProjectFixture(t)
	want(t, f.owner.post(t, "/api/v1/projects", map[string]any{"key": key, "name": "Lookups"}), http.StatusCreated, "make a project")
	// The third is one a walk would read and a lookup by id would not.
	for _, title := range []string{"Brakes engage", "Brakes hold", "Brakes release"} {
		want(t, f.owner.post(t, "/api/v1/projects/"+key+"/requirements", map[string]any{"title": title}), http.StatusCreated, "write "+title)
	}
	link := obj(t, want(t, f.owner.post(t, "/api/v1/requirements/"+key+"-R1/links", map[string]any{"typeKey": "derives", "otherKey": key + "-R2"}),
		http.StatusCreated, "link "+key+"-R1 to "+key+"-R2"), "link")
	id, err := uuid.Parse(link["id"].(string))
	if err != nil {
		t.Fatal(err)
	}
	f.h.settle(t)

	lookups := &linkLookups{}
	ctx := context.Background()
	cluster, err := db.Open(ctx, f.h.cfg.DB, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})),
		db.WithQueryTracer(func(string) pgx.QueryTracer { return lookups }))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cluster.Close)
	if _, err := trace.NewService(cluster).Unlink(db.WithUser(ctx, f.home.user), f.home.user, id); err != nil {
		t.Fatalf("remove the link: %v", err)
	}
	if len(lookups.seen) == 0 {
		t.Fatal("removing the link read no link with its ends")
	}

	for _, q := range lookups.seen {
		var out []byte
		err := f.h.cluster.ReadPrimary(f.home.ctx, func(ctx context.Context, tx db.DBTX) error {
			// The plan a hub's skewed statistics led to: nothing is joined by looping over the link's ends.
			if _, err := tx.Exec(ctx, `SET LOCAL enable_nestloop = off`); err != nil {
				return err
			}
			// The link is gone by now, and a removed link is read as far as its ends all the same.
			sql := strings.Replace(q.SQL, "l.deleted_at IS NULL", "l.deleted_at IS NOT NULL", 1)
			return tx.QueryRow(ctx, "EXPLAIN (ANALYZE, FORMAT JSON) "+sql, q.Args...).Scan(&out)
		})
		if err != nil {
			t.Fatalf("explain the lookup: %v", err)
		}
		var plans []struct {
			Plan planNode `json:"Plan"`
		}
		if err := json.Unmarshal(out, &plans); err != nil || len(plans) != 1 {
			t.Fatalf("read the plan %s: %v", out, err)
		}
		if n := plans[0].Plan.rowsRead("requirement"); n != endsPerLink {
			t.Errorf("reading the link read %v requirement rows, want %d, one per end:\n%s\n%s", n, endsPerLink, q.SQL, out)
		}
	}
}
