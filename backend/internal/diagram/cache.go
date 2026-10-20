package diagram

import (
	"container/list"
	"crypto/sha256"
	"sync"
)

// cacheKey is the hash of what was drawn: language, format and source.
type cacheKey = [sha256.Size]byte

// cache keeps the pictures drawn last, up to a number of bytes, so a page read again asks the renderer nothing.
type cache struct {
	mu    sync.Mutex
	limit int64
	size  int64
	order *list.List
	items map[cacheKey]*list.Element
}

type entry struct {
	key     cacheKey
	picture []byte
}

func newCache(limit int64) *cache {
	return &cache{limit: limit, order: list.New(), items: map[cacheKey]*list.Element{}}
}

func (c *cache) get(key cacheKey) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.items[key]
	if !ok {
		return nil, false
	}
	c.order.MoveToFront(el)
	return el.Value.(*entry).picture, true
}

// put keeps a picture, dropping the least recently read ones it no longer fits beside; one larger than the whole
// cache is not kept.
func (c *cache) put(key cacheKey, picture []byte) {
	size := int64(len(picture))
	if size > c.limit {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.items[key]; ok {
		return
	}
	c.items[key] = c.order.PushFront(&entry{key: key, picture: picture})
	c.size += size
	for c.size > c.limit {
		last := c.order.Back()
		e := last.Value.(*entry)
		c.order.Remove(last)
		delete(c.items, e.key)
		c.size -= int64(len(e.picture))
	}
}
