package store

import (
	"errors"
	"testing"
)

func TestCachedBlobStorePutThenGetHitsCacheNotUnderlying(t *testing.T) {
	counting := newCountingBlobStore(NewMemoryBlobStore())
	c := NewCachedBlobStore(counting, 1<<20)

	if err := c.Put("b1", "h1", []byte("hello")); err != nil {
		t.Fatalf("Put: %v", err)
	}

	for range 3 {
		data, err := c.Get("b1", "h1")
		if err != nil || string(data) != "hello" {
			t.Fatalf("Get = (%q, %v), want (hello, nil)", data, err)
		}
	}

	if got := counting.count(counting.puts, "b1", "h1"); got != 1 {
		t.Errorf("underlying Put called %d times, want 1", got)
	}
	// Put also warms the cache, so none of the three Gets above should have
	// reached the underlying store.
	if got := counting.count(counting.gets(), "b1", "h1"); got != 0 {
		t.Errorf("underlying Get called %d times, want 0 (cache should have served all reads)", got)
	}
}

func TestCachedBlobStoreGetMissFallsThroughAndPopulatesCache(t *testing.T) {
	underlying := NewMemoryBlobStore()
	if err := underlying.Put("b1", "h1", []byte("hello")); err != nil {
		t.Fatalf("seed Put: %v", err)
	}
	counting := newCountingBlobStore(underlying)
	c := NewCachedBlobStore(counting, 1<<20)

	// First Get is a cache miss and must fall through to the underlying store.
	if data, err := c.Get("b1", "h1"); err != nil || string(data) != "hello" {
		t.Fatalf("first Get = (%q, %v), want (hello, nil)", data, err)
	}
	// Second Get should now be served from cache.
	if data, err := c.Get("b1", "h1"); err != nil || string(data) != "hello" {
		t.Fatalf("second Get = (%q, %v), want (hello, nil)", data, err)
	}

	if got := counting.count(counting.gets(), "b1", "h1"); got != 1 {
		t.Errorf("underlying Get called %d times, want 1 (only the initial miss)", got)
	}
}

func TestCachedBlobStoreDeleteInvalidatesCache(t *testing.T) {
	c := NewCachedBlobStore(NewMemoryBlobStore(), 1<<20)

	if err := c.Put("b1", "h1", []byte("hello")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if _, err := c.Get("b1", "h1"); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if err := c.Delete("b1", "h1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := c.Get("b1", "h1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get after Delete: err = %v, want ErrNotFound", err)
	}
}

func TestCachedBlobStoreFallsBackWhenEvicted(t *testing.T) {
	underlying := NewMemoryBlobStore()
	counting := newCountingBlobStore(underlying)
	// Small enough that a second, different-key blob evicts the first.
	c := NewCachedBlobStore(counting, 8)

	if err := c.Put("b1", "h1", []byte("12345678")); err != nil {
		t.Fatalf("Put h1: %v", err)
	}
	if err := c.Put("b1", "h2", []byte("87654321")); err != nil {
		t.Fatalf("Put h2: %v", err)
	}

	// h1 should have been evicted from the cache by h2, so this Get must
	// fall through to (and succeed via) the underlying store.
	data, err := c.Get("b1", "h1")
	if err != nil || string(data) != "12345678" {
		t.Fatalf("Get h1 after eviction = (%q, %v), want (12345678, nil)", data, err)
	}
	if got := counting.count(counting.gets(), "b1", "h1"); got != 1 {
		t.Errorf("underlying Get(h1) called %d times, want 1 (cache should have evicted it)", got)
	}
}
