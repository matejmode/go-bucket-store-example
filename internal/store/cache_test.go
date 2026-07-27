package store

import "testing"

func TestByteBoundedLRUEvictsOldestWhenOverBudget(t *testing.T) {
	c := newByteBoundedLRU(10)

	c.Put("a", []byte("12345")) // 5 bytes, total 5
	c.Put("b", []byte("12345")) // 5 bytes, total 10

	if _, ok := c.Get("a"); !ok {
		t.Fatalf("expected a to still be cached")
	}

	// "a" is now most-recently-used (touched by Get above); adding "c" should
	// evict "b", the least recently used entry.
	c.Put("c", []byte("12345")) // total would be 15, over budget of 10

	if _, ok := c.Get("b"); ok {
		t.Errorf("expected b to have been evicted")
	}
	if _, ok := c.Get("a"); !ok {
		t.Errorf("expected a to still be cached")
	}
	if _, ok := c.Get("c"); !ok {
		t.Errorf("expected c to be cached")
	}
}

func TestByteBoundedLRUDelete(t *testing.T) {
	c := newByteBoundedLRU(100)
	c.Put("a", []byte("data"))
	c.Delete("a")
	if _, ok := c.Get("a"); ok {
		t.Errorf("expected a to be gone after Delete")
	}
}

func TestByteBoundedLRUOverwriteUpdatesSize(t *testing.T) {
	c := newByteBoundedLRU(10)
	c.Put("a", []byte("12345"))      // 5 bytes
	c.Put("a", []byte("1234567890")) // 10 bytes, overwrite same key
	if c.curBytes != 10 {
		t.Errorf("curBytes = %d, want 10", c.curBytes)
	}
	data, ok := c.Get("a")
	if !ok || string(data) != "1234567890" {
		t.Errorf("Get = (%q, %v), want (1234567890, true)", data, ok)
	}
}
