package attachment

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/objectstore"
)

// tombstones stands in for the attachment_tombstone table: the keys read, and those a write deletes.
type tombstones struct{ keys []string }

func (s *tombstones) Exec(_ context.Context, _ string, args ...any) (pgconn.CommandTag, error) {
	key := args[0].(string)
	s.keys = slices.DeleteFunc(s.keys, func(k string) bool { return k == key })
	return pgconn.NewCommandTag("DELETE 1"), nil
}

func (s *tombstones) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return &keyRows{keys: slices.Clone(s.keys), at: -1}, nil
}

func (s *tombstones) QueryRow(context.Context, string, ...any) pgx.Row { return nil }

func (s *tombstones) read(ctx context.Context, fn func(context.Context, db.DBTX) error) error {
	return fn(ctx, s)
}

func (s *tombstones) write(ctx context.Context, fn func(context.Context, db.DBTX) error) (db.LSN, error) {
	return 0, fn(ctx, s)
}

// keyRows are the object keys a read of the tombstones answers; what the reaper does not call is left unbuilt.
type keyRows struct {
	pgx.Rows
	keys []string
	at   int
}

func (r *keyRows) Close()                 {}
func (r *keyRows) Err() error             { return nil }
func (r *keyRows) Next() bool             { r.at++; return r.at < len(r.keys) }
func (r *keyRows) Scan(dest ...any) error { *dest[0].(*string) = r.keys[r.at]; return nil }

// stubborn is a store that refuses to delete one object.
type stubborn struct {
	*objectstore.Memory
	refuses string
}

var errStoreDown = errors.New("the store is down")

func (s *stubborn) Delete(ctx context.Context, key string) error {
	if key == s.refuses {
		return errStoreDown
	}
	return s.Memory.Delete(ctx, key)
}

func TestTheReaperKeepsATombstoneUntilItsObjectIsGone(t *testing.T) {
	ctx := context.Background()
	store := &stubborn{Memory: objectstore.NewMemory(), refuses: "org/x/stuck"}
	for _, key := range []string{"org/x/loose", "org/x/stuck"} {
		if err := store.Put(ctx, key, bytes.NewReader([]byte("x")), 1, "text/plain"); err != nil {
			t.Fatal(err)
		}
	}
	graves := &tombstones{keys: []string{"org/x/loose", "org/x/stuck"}}
	s := &Service{store: store, log: slog.New(slog.DiscardHandler)}

	removed, err := s.reapBatchWith(ctx, graves.read, graves.write)
	if err != nil || removed != 1 {
		t.Fatalf("the first pass removed %d: %v", removed, err)
	}
	if !slices.Equal(graves.keys, []string{"org/x/stuck"}) {
		t.Errorf("after the first pass the tombstones are %v, want the stuck one kept for the next run", graves.keys)
	}
	if store.Len() != 1 {
		t.Errorf("%d objects are left, want the stuck one", store.Len())
	}

	store.refuses = ""
	if removed, err := s.reapBatchWith(ctx, graves.read, graves.write); err != nil || removed != 1 {
		t.Fatalf("the next pass removed %d: %v", removed, err)
	}
	if len(graves.keys) != 0 || store.Len() != 0 {
		t.Errorf("after the store came back %v tombstones and %d objects are left", graves.keys, store.Len())
	}
}
