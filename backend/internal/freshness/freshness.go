// Package freshness remembers how far a reader's own writes reached, so their
// next reads are never served by a replica that has not replayed them yet.
package freshness

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/cloudster1/exciter/backend/internal/db"
)

// Prefix keeps the api's read-your-writes keys apart from anything else in Valkey; every api process shares it.
const Prefix = "exciter:ryw:"

// storeTimeout bounds each round trip to Valkey: a slow store must cost a
// request a few milliseconds at most, never its deadline.
const storeTimeout = 250 * time.Millisecond

// Tracker records and recalls the last write position seen for a key.
type Tracker interface {
	// Note records that the holder of key has just written up to lsn.
	Note(ctx context.Context, key string, lsn db.LSN)
	// Required is the position a replica must have replayed to serve key, or
	// zero when there is no recent write to respect.
	Required(ctx context.Context, key string) db.LSN
}

// ValkeyTracker shares positions across every api process, which is what
// keeps a write on one process visible to a read that lands on another.
type ValkeyTracker struct {
	client *redis.Client
	ttl    time.Duration
	prefix string
}

// NewValkeyTracker keeps each position for ttl under keys that start with prefix.
func NewValkeyTracker(client *redis.Client, ttl time.Duration, prefix string) *ValkeyTracker {
	return &ValkeyTracker{client: client, ttl: ttl, prefix: prefix}
}

// noteScript keeps the newer of two positions: requests finish out of order.
// Positions are fixed width hex, since a Lua number would round a large one.
var noteScript = redis.NewScript(`
local cur = redis.call('GET', KEYS[1])
if cur and cur >= ARGV[1] then
  redis.call('PEXPIRE', KEYS[1], ARGV[2])
  return 0
end
redis.call('SET', KEYS[1], ARGV[1], 'PX', ARGV[2])
return 1
`)

func encode(lsn db.LSN) string { return fmt.Sprintf("%016X", uint64(lsn)) }

// Note is best effort: a failure costs a stale read at worst, and must never
// fail a write that has already committed.
func (t *ValkeyTracker) Note(ctx context.Context, key string, lsn db.LSN) {
	if key == "" || lsn == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), storeTimeout)
	defer cancel()
	_ = noteScript.Run(ctx, t.client, []string{t.prefix + key}, encode(lsn), t.ttl.Milliseconds()).Err()
}

func (t *ValkeyTracker) Required(ctx context.Context, key string) db.LSN {
	if key == "" {
		return 0
	}
	ctx, cancel := context.WithTimeout(ctx, storeTimeout)
	defer cancel()
	v, err := t.client.Get(ctx, t.prefix+key).Result()
	if err != nil {
		return 0
	}
	var lsn uint64
	if _, err := fmt.Sscanf(v, "%X", &lsn); err != nil {
		return 0
	}
	return db.LSN(lsn)
}

// Ping checks the store answers, so a misconfigured URL stops the boot.
func (t *ValkeyTracker) Ping(ctx context.Context) error {
	return t.client.Ping(ctx).Err()
}

type entry struct {
	lsn db.LSN
	exp time.Time
}

// sweepAbove is how many entries MemoryTracker holds before it drops the
// expired ones, so an endless stream of keys cannot grow it forever.
const sweepAbove = 1024

// MemoryTracker is the single process tracker, for tests and for one api
// process running without Valkey.
type MemoryTracker struct {
	mu      sync.Mutex
	entries map[string]entry
	ttl     time.Duration
	now     func() time.Time
}

// NewMemoryTracker keeps each position for ttl.
func NewMemoryTracker(ttl time.Duration) *MemoryTracker {
	return &MemoryTracker{entries: make(map[string]entry), ttl: ttl, now: time.Now}
}

func (t *MemoryTracker) Note(_ context.Context, key string, lsn db.LSN) {
	if key == "" || lsn == 0 {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	if cur, ok := t.entries[key]; ok && cur.exp.After(now) && cur.lsn >= lsn {
		lsn = cur.lsn
	}
	t.entries[key] = entry{lsn: lsn, exp: now.Add(t.ttl)}
	if len(t.entries) > sweepAbove {
		for k, e := range t.entries {
			if !e.exp.After(now) {
				delete(t.entries, k)
			}
		}
	}
}

func (t *MemoryTracker) Required(_ context.Context, key string) db.LSN {
	if key == "" {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	e, ok := t.entries[key]
	if !ok || !e.exp.After(t.now()) {
		return 0
	}
	return e.lsn
}
