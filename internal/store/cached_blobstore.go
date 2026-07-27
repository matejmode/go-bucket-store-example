package store

// CachedBlobStore wraps a BlobStore with a read-through/write-through
// byte-bounded LRU cache. Cache keys are stable (content-addressed by hash),
// so there's no invalidation to reason about - a cached entry is either
// present (and correct) or evicted.
type CachedBlobStore struct {
	next  BlobStore
	cache *byteBoundedLRU
}

// NewCachedBlobStore wraps next with an LRU cache capped at maxCacheBytes
// total bytes.
func NewCachedBlobStore(next BlobStore, maxCacheBytes int64) *CachedBlobStore {
	return &CachedBlobStore{next: next, cache: newByteBoundedLRU(maxCacheBytes)}
}

func cacheKey(bucket, hash string) string {
	return bucket + "/" + hash
}

// Put writes through to the underlying BlobStore, then warms the cache
// with the same bytes.
func (c *CachedBlobStore) Put(bucket, hash string, data []byte) error {
	if err := c.next.Put(bucket, hash, data); err != nil {
		return err
	}
	c.cache.Put(cacheKey(bucket, hash), data)
	return nil
}

// Get returns the cached value for (bucket, hash) if present; otherwise it
// reads through to the underlying BlobStore and populates the cache before
// returning.
func (c *CachedBlobStore) Get(bucket, hash string) ([]byte, error) {
	if data, ok := c.cache.Get(cacheKey(bucket, hash)); ok {
		return data, nil
	}
	data, err := c.next.Get(bucket, hash)
	if err != nil {
		return nil, err
	}
	c.cache.Put(cacheKey(bucket, hash), data)
	return data, nil
}

// Delete evicts (bucket, hash) from the cache, then deletes it from the
// underlying BlobStore.
func (c *CachedBlobStore) Delete(bucket, hash string) error {
	c.cache.Delete(cacheKey(bucket, hash))
	return c.next.Delete(bucket, hash)
}
