package store

import (
	"container/list"
	"sync"
)

// byteBoundedLRU is a simple in-process LRU cache bounded by total bytes
// held, rather than entry count - a handful of large objects shouldn't be
// able to evict everything else while still under a low entry-count cap.
type byteBoundedLRU struct {
	mu       sync.Mutex
	maxBytes int64
	curBytes int64
	ll       *list.List // front = most recently used
	items    map[string]*list.Element
}

type lruEntry struct {
	key  string
	data []byte
}

func newByteBoundedLRU(maxBytes int64) *byteBoundedLRU {
	return &byteBoundedLRU{
		maxBytes: maxBytes,
		ll:       list.New(),
		items:    make(map[string]*list.Element),
	}
}

// Get returns the cached value for key and marks it most-recently-used, or
// reports false if key isn't cached.
func (c *byteBoundedLRU) Get(key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.items[key]
	if !ok {
		return nil, false
	}
	c.ll.MoveToFront(el)
	return el.Value.(*lruEntry).data, true
}

// Put stores data under key as most-recently-used, evicting
// least-recently-used entries until the cache is back within maxBytes.
func (c *byteBoundedLRU) Put(key string, data []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if el, ok := c.items[key]; ok {
		c.curBytes -= int64(len(el.Value.(*lruEntry).data))
		el.Value.(*lruEntry).data = data
		c.curBytes += int64(len(data))
		c.ll.MoveToFront(el)
	} else {
		el := c.ll.PushFront(&lruEntry{key: key, data: data})
		c.items[key] = el
		c.curBytes += int64(len(data))
	}

	for c.curBytes > c.maxBytes && c.ll.Len() > 0 {
		back := c.ll.Back()
		if back == nil {
			break
		}
		entry := back.Value.(*lruEntry)
		c.curBytes -= int64(len(entry.data))
		delete(c.items, entry.key)
		c.ll.Remove(back)
	}
}

// Delete removes key from the cache, if present.
func (c *byteBoundedLRU) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[key]; ok {
		c.curBytes -= int64(len(el.Value.(*lruEntry).data))
		delete(c.items, key)
		c.ll.Remove(el)
	}
}
