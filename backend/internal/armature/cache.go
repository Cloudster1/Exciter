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

// CachePrefix starts every key the cache writes; the organization follows.
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

func orgKey(org uuid.UUID, rest string) string {
	return CachePrefix + org.String() + ":" + rest
}

// IssueKey is the hash holding one issue key's answers, one field per token row.
func IssueKey(org uuid.UUID, issueKey string) string {
	return orgKey(org, "issue:"+strings.ToUpper(issueKey))
}

// SearchKey is the hash holding every search's answers in an organization.
func SearchKey(org uuid.UUID) string { return orgKey(org, "search") }

// SearchField names one person's answer to one query and page.
func SearchField(tokenID uuid.UUID, query string, limit, offset int) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%d\x00%d", query, limit, offset)))
	return tokenID.String() + ":" + hex.EncodeToString(sum[:])
}

// MetaKey holds one person's projects and issue types.
func MetaKey(org, tokenID uuid.UUID) string { return orgKey(org, "meta:"+tokenID.String()) }

// ThemeKey holds the Armature theme one person sees.
func ThemeKey(org, tokenID uuid.UUID) string { return orgKey(org, "theme:"+tokenID.String()) }

// EventKey marks a webhook event as acted on.
func EventKey(org, eventID uuid.UUID) string { return orgKey(org, "event:"+eventID.String()) }

// Issue reads the owner of tokenID's answer for issueKey into out, null for
// an issue they may not see; false is a miss, a field past its TTL included.
func (c *Cache) Issue(ctx context.Context, org, tokenID uuid.UUID, issueKey string, out any) (ok bool) {
	return c.getField(ctx, IssueKey(org, issueKey), tokenID.String(), IssueCacheTTL, out)
}

// PutIssue stores value, which may be nil, as the owner of tokenID's answer
// for issueKey.
func (c *Cache) PutIssue(ctx context.Context, org, tokenID uuid.UUID, issueKey string, value any) {
	c.putField(ctx, IssueKey(org, issueKey), tokenID.String(), IssueCacheTTL, value)
}

// Search reads a cached search answer; see Issue.
func (c *Cache) Search(ctx context.Context, org, tokenID uuid.UUID, query string, limit, offset int, out any) bool {
	return c.getField(ctx, SearchKey(org), SearchField(tokenID, query, limit, offset), SearchCacheTTL, out)
}

// PutSearch stores a search answer.
func (c *Cache) PutSearch(ctx context.Context, org, tokenID uuid.UUID, query string, limit, offset int, value any) {
	c.putField(ctx, SearchKey(org), SearchField(tokenID, query, limit, offset), SearchCacheTTL, value)
}

// Meta reads a person's cached projects and issue types into out.
func (c *Cache) Meta(ctx context.Context, org, tokenID uuid.UUID, out any) bool {
	return c.get(ctx, MetaKey(org, tokenID), MetaCacheTTL, out)
}

// PutMeta stores a person's projects and issue types.
func (c *Cache) PutMeta(ctx context.Context, org, tokenID uuid.UUID, value any) {
	c.put(ctx, MetaKey(org, tokenID), MetaCacheTTL, value)
}

// Theme reads the Armature theme a person saw last into out.
func (c *Cache) Theme(ctx context.Context, org, tokenID uuid.UUID, out any) bool {
	return c.get(ctx, ThemeKey(org, tokenID), ThemeCacheTTL, out)
}

// PutTheme stores the Armature theme a person sees, or nil for the built-in one.
func (c *Cache) PutTheme(ctx context.Context, org, tokenID uuid.UUID, value any) {
	c.put(ctx, ThemeKey(org, tokenID), ThemeCacheTTL, value)
}

// ForgetIssues clears every person's answers for the issue keys.
func (c *Cache) ForgetIssues(ctx context.Context, org uuid.UUID, issueKeys ...string) {
	if c == nil || len(issueKeys) == 0 {
		return
	}
	keys := make([]string, len(issueKeys))
	for i, k := range issueKeys {
		keys[i] = IssueKey(org, k)
	}
	c.warn(c.store.Del(ctx, keys...).Err(), "forget issues")
}

// ForgetSearches clears every search answer in the organization.
func (c *Cache) ForgetSearches(ctx context.Context, org uuid.UUID) {
	if c == nil {
		return
	}
	c.warn(c.store.Del(ctx, SearchKey(org)).Err(), "forget searches")
}

// FirstDelivery marks a webhook event as acted on and says whether it is the
// first time; without a cache every delivery is.
func (c *Cache) FirstDelivery(ctx context.Context, org, eventID uuid.UUID) bool {
	// The worst a replay does then is clear entries that are not there.
	if c == nil {
		return true
	}
	first, err := c.store.SetNX(ctx, EventKey(org, eventID), "1", WebhookReplayWindow).Result()
	if err != nil {
		c.warn(err, "remember a webhook event")
		return true
	}
	return first
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
