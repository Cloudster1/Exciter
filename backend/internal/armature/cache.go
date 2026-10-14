package armature

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// CachePrefix starts every key the cache writes.
const CachePrefix = "exciter:armature:"

// Cache keeps Armature's answers per person in Valkey, keyed by their token
// row's id; a nil Cache keeps nothing. See docs/api-contract-m2.md.
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

// IssueKey is the hash holding one issue key's answers, one field per token row.
func IssueKey(issueKey string) string {
	return cacheKey("issue:" + strings.ToUpper(issueKey))
}

// SearchKey is the hash holding every search's answers.
func SearchKey() string { return cacheKey("search") }

// SearchField names one person's answer to one query and page.
func SearchField(tokenID uuid.UUID, query string, limit, offset int) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%d\x00%d", query, limit, offset)))
	return tokenID.String() + ":" + hex.EncodeToString(sum[:])
}

// MetaKey holds one person's projects and issue types.
func MetaKey(tokenID uuid.UUID) string { return cacheKey("meta:" + tokenID.String()) }

// ThemeKey holds the Armature theme one person sees.
func ThemeKey(tokenID uuid.UUID) string { return cacheKey("theme:" + tokenID.String()) }

// EventKey marks a webhook event as acted on.
func EventKey(eventID uuid.UUID) string { return cacheKey("event:" + eventID.String()) }

// Issue reads the owner of tokenID's answer for issueKey into out, null for
// an issue they may not see; false is a miss, a field past its TTL included.
func (c *Cache) Issue(ctx context.Context, tokenID uuid.UUID, issueKey string, out any) (ok bool) {
	return c.getField(ctx, IssueKey(issueKey), tokenID.String(), IssueCacheTTL, out)
}

// PutIssue stores value, which may be nil, as the owner of tokenID's answer
// for issueKey.
func (c *Cache) PutIssue(ctx context.Context, tokenID uuid.UUID, issueKey string, value any) {
	c.putField(ctx, IssueKey(issueKey), tokenID.String(), IssueCacheTTL, value)
}

// Search reads a cached search answer; see Issue.
func (c *Cache) Search(ctx context.Context, tokenID uuid.UUID, query string, limit, offset int, out any) bool {
	return c.getField(ctx, SearchKey(), SearchField(tokenID, query, limit, offset), SearchCacheTTL, out)
}

// PutSearch stores a search answer.
func (c *Cache) PutSearch(ctx context.Context, tokenID uuid.UUID, query string, limit, offset int, value any) {
	c.putField(ctx, SearchKey(), SearchField(tokenID, query, limit, offset), SearchCacheTTL, value)
}

// Meta reads a person's cached projects and issue types into out.
func (c *Cache) Meta(ctx context.Context, tokenID uuid.UUID, out any) bool {
	return c.get(ctx, MetaKey(tokenID), MetaCacheTTL, out)
}

// PutMeta stores a person's projects and issue types.
func (c *Cache) PutMeta(ctx context.Context, tokenID uuid.UUID, value any) {
	c.put(ctx, MetaKey(tokenID), MetaCacheTTL, value)
}

// Theme reads the Armature theme a person saw last into out.
func (c *Cache) Theme(ctx context.Context, tokenID uuid.UUID, out any) bool {
	return c.get(ctx, ThemeKey(tokenID), ThemeCacheTTL, out)
}

// PutTheme stores the Armature theme a person sees, or nil for the built-in one.
func (c *Cache) PutTheme(ctx context.Context, tokenID uuid.UUID, value any) {
	c.put(ctx, ThemeKey(tokenID), ThemeCacheTTL, value)
}

// ForgetIssues clears every person's answers for the issue keys.
func (c *Cache) ForgetIssues(ctx context.Context, issueKeys ...string) {
	if c == nil || len(issueKeys) == 0 {
		return
	}
	keys := make([]string, len(issueKeys))
	for i, k := range issueKeys {
		keys[i] = IssueKey(k)
	}
	c.warn(c.store.Del(ctx, keys...).Err(), "forget issues")
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

func (c *Cache) get(ctx context.Context, key string, ttl time.Duration, out any) bool {
	if c == nil {
		return false
	}
	raw, err := c.store.Get(ctx, key).Bytes()
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

func (c *Cache) put(ctx context.Context, key string, ttl time.Duration, value any) {
	if c == nil {
		return
	}
	if raw, ok := c.encode(value); ok {
		c.warn(c.store.Set(ctx, key, raw, ttl).Err(), "write")
	}
}

func (c *Cache) warn(err error, what string) {
	if err != nil {
		c.log.Warn("the Armature cache failed; Armature is asked instead", "step", what, "error", err)
	}
}
