package trace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/cloudster1/exciter/backend/internal/search"
)

const (
	// CoverageCachePrefix starts every key the coverage cache writes.
	CoverageCachePrefix = "exciter:coverage:"
	// CoverageCacheTTL is how long a project's coverage is answered from the cache at most.
	CoverageCacheTTL = time.Minute
)

// CoverageCache keeps each reader's coverage of a project per filter in Valkey, one hash per project, so one
// change clears the project's whole coverage at once; a nil CoverageCache keeps nothing.
type CoverageCache struct {
	store redis.UniversalClient
	log   *slog.Logger
	now   func() time.Time
}

// NewCoverageCache returns a cache over store, or nil for a nil store.
func NewCoverageCache(store redis.UniversalClient, log *slog.Logger) *CoverageCache {
	if store == nil {
		return nil
	}
	if log == nil {
		log = slog.Default()
	}
	return &CoverageCache{store: store, log: log, now: time.Now}
}

// CoverageKey is the hash holding one project's coverage.
func CoverageKey(projectID uuid.UUID) string { return CoverageCachePrefix + projectID.String() }

// coverageField names one reader's coverage under one filter: what the reader sees decides what is counted.
func coverageField(reader uuid.UUID, f search.Filter) string {
	raw, _ := json.Marshal(f)
	sum := sha256.Sum256(append([]byte(reader.String()+"\x00"), raw...))
	return hex.EncodeToString(sum[:])
}

// coverageEntry carries its own age: a busy hash keeps its expiry pushed out by every write.
type coverageEntry struct {
	At   time.Time     `json:"at"`
	Rows []CoverageRow `json:"rows"`
}

// Get reads the rows a reader's coverage of a project under f was last; false is a miss.
func (c *CoverageCache) Get(ctx context.Context, projectID, reader uuid.UUID, f search.Filter) ([]CoverageRow, bool) {
	if c == nil {
		return nil, false
	}
	raw, err := c.store.HGet(ctx, CoverageKey(projectID), coverageField(reader, f)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, false
	}
	var e coverageEntry
	if err != nil || json.Unmarshal(raw, &e) != nil {
		c.warn(err, "read")
		return nil, false
	}
	if age := c.now().Sub(e.At); age < 0 || age >= CoverageCacheTTL {
		return nil, false
	}
	return e.Rows, true
}

// Put stores the rows of a reader's coverage of a project under f.
func (c *CoverageCache) Put(ctx context.Context, projectID, reader uuid.UUID, f search.Filter, rows []CoverageRow) {
	if c == nil {
		return
	}
	raw, err := json.Marshal(coverageEntry{At: c.now(), Rows: rows})
	if err != nil {
		c.warn(err, "encode")
		return
	}
	pipe := c.store.TxPipeline()
	pipe.HSet(ctx, CoverageKey(projectID), coverageField(reader, f), raw)
	pipe.Expire(ctx, CoverageKey(projectID), CoverageCacheTTL)
	_, err = pipe.Exec(ctx)
	c.warn(err, "write")
}

// Forget clears the coverage of the projects named, for every reader and filter.
func (c *CoverageCache) Forget(ctx context.Context, projectIDs ...uuid.UUID) {
	if c == nil || len(projectIDs) == 0 {
		return
	}
	keys := make([]string, len(projectIDs))
	for i, id := range projectIDs {
		keys[i] = CoverageKey(id)
	}
	c.warn(c.store.Del(ctx, keys...).Err(), "forget")
}

func (c *CoverageCache) warn(err error, what string) {
	if err != nil {
		c.log.Warn("the coverage cache failed; coverage is read from the database instead", "step", what, "error", err)
	}
}
