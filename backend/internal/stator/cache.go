package stator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// CachePrefix starts every key the cache writes.
const CachePrefix = "exciter:stator:"

// Cache keeps Stator's answers per person in Valkey, keyed by their token row's id, as Armature's cache does; a nil
// Cache keeps nothing. See docs/api-contract-m4.md.
type Cache struct {
	// Only Valkey, never a copy in the process: a webhook reaching one api
	// process could not clear the others. An error there is a miss, logged.
	store redis.UniversalClient
	log   *slog.Logger
	// now is the clock the ages of entries are judged by.
	now func() time.Time
}

// NewCache returns a cache over store, or nil for a nil store.
func NewCache(store redis.UniversalClient, log *slog.Logger) *Cache {
	if store == nil {
		return nil
	}
	if log == nil {
		log = slog.Default()
	}
	return &Cache{store: store, log: log, now: time.Now}
}

// entry is what every field and key holds: when it was stored, and the value.
type entry struct {
	At    time.Time       `json:"at"`
	Value json.RawMessage `json:"value"`
}

func cacheKey(rest string) string {
	return CachePrefix + rest
}

// PageKey is the hash holding one page's answers, one field per token row.
func PageKey(pageID uuid.UUID) string {
	return cacheKey("page:" + pageID.String())
}

// SearchKey is the hash holding every search's answers.
func SearchKey() string { return cacheKey("search") }

// SearchField names one person's answer to one query, space and limit.
func SearchField(tokenID uuid.UUID, query, space string, limit int) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%d", query, space, limit)))
	return tokenID.String() + ":" + hex.EncodeToString(sum[:])
}

// EventKey marks a webhook event as acted on.
func EventKey(eventID uuid.UUID) string { return cacheKey("event:" + eventID.String()) }

// Page reads the owner of tokenID's answer for a page into out, null for a page they may not read; false is a
// miss, a field past its TTL included.
func (c *Cache) Page(ctx context.Context, tokenID, pageID uuid.UUID, out any) (ok bool) {
	return c.getField(ctx, PageKey(pageID), tokenID.String(), PageCacheTTL, out)
}

// PutPage stores value, which may be nil, as the owner of tokenID's answer for a page.
func (c *Cache) PutPage(ctx context.Context, tokenID, pageID uuid.UUID, value any) {
	c.putField(ctx, PageKey(pageID), tokenID.String(), PageCacheTTL, value)
}

// Search reads a cached search answer; see Page.
func (c *Cache) Search(ctx context.Context, tokenID uuid.UUID, query, space string, limit int, out any) bool {
	return c.getField(ctx, SearchKey(), SearchField(tokenID, query, space, limit), SearchCacheTTL, out)
}

// PutSearch stores a search answer.
func (c *Cache) PutSearch(ctx context.Context, tokenID uuid.UUID, query, space string, limit int, value any) {
	c.putField(ctx, SearchKey(), SearchField(tokenID, query, space, limit), SearchCacheTTL, value)
}

// ForgetPages clears every person's answers for the pages.
func (c *Cache) ForgetPages(ctx context.Context, pageIDs ...uuid.UUID) {
	if c == nil || len(pageIDs) == 0 {
		return
	}
	keys := make([]string, len(pageIDs))
	for i, id := range pageIDs {
		keys[i] = PageKey(id)
	}
	c.warn(c.store.Del(ctx, keys...).Err(), "forget pages")
}

// ForgetSearches clears every search answer in the installation.
func (c *Cache) ForgetSearches(ctx context.Context) {
	if c == nil {
		return
	}
	c.warn(c.store.Del(ctx, SearchKey()).Err(), "forget searches")
}

// Delivered says whether a webhook event was acted on already; without a cache, or when Valkey does not answer,
// none was, and acting again changes nothing a first time did not.
func (c *Cache) Delivered(ctx context.Context, eventID uuid.UUID) bool {
	if c == nil {
		return false
	}
	n, err := c.store.Exists(ctx, EventKey(eventID)).Result()
	c.warn(err, "read a webhook event")
	return n > 0
}

// NoteDelivered remembers that a webhook event was acted on, for WebhookReplayWindow.
func (c *Cache) NoteDelivered(ctx context.Context, eventID uuid.UUID) {
	if c == nil {
		return
	}
	c.warn(c.store.Set(ctx, EventKey(eventID), "1", WebhookReplayWindow).Err(), "remember a webhook event")
}

func (c *Cache) getField(ctx context.Context, key, field string, ttl time.Duration, out any) bool {
	if c == nil {
		return false
	}
	raw, err := c.store.HGet(ctx, key, field).Bytes()
	return c.decode(raw, err, ttl, out)
}

// decode judges an entry's age itself: a busy hash keeps its expiry pushed
// out by every write, so an old field in it would otherwise live on.
func (c *Cache) decode(raw []byte, err error, ttl time.Duration, out any) bool {
	if errors.Is(err, redis.Nil) {
		return false
	}
	if err != nil {
		c.warn(err, "read")
		return false
	}
	var e entry
	if err := json.Unmarshal(raw, &e); err != nil {
		c.warn(err, "decode")
		return false
	}
	if age := c.now().Sub(e.At); age < 0 || age >= ttl {
		return false
	}
	if err := json.Unmarshal(e.Value, out); err != nil {
		c.warn(err, "decode")
		return false
	}
	return true
}

func (c *Cache) encode(value any) ([]byte, bool) {
	inner, err := json.Marshal(value)
	if err != nil {
		c.warn(err, "encode")
		return nil, false
	}
	raw, err := json.Marshal(entry{At: c.now(), Value: inner})
	if err != nil {
		c.warn(err, "encode")
		return nil, false
	}
	return raw, true
}

func (c *Cache) putField(ctx context.Context, key, field string, ttl time.Duration, value any) {
	if c == nil {
		return
	}
	raw, ok := c.encode(value)
	if !ok {
		return
	}
	pipe := c.store.TxPipeline()
	pipe.HSet(ctx, key, field, raw)
	pipe.Expire(ctx, key, ttl)
	_, err := pipe.Exec(ctx)
	c.warn(err, "write")
}

func (c *Cache) warn(err error, what string) {
	if err != nil {
		c.log.Warn("the Stator cache failed; Stator is asked instead", "step", what, "error", err)
	}
}
