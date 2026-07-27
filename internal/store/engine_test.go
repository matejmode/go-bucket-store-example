package store

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

// countingBlobStore wraps a BlobStore and counts Put/Get/Delete calls per
// (bucket, hash) key, so tests can assert a shared blob is written and
// deleted exactly once regardless of how many objectIDs reference it, or
// that a caching layer in front of it is actually avoiding redundant calls.
type countingBlobStore struct {
	BlobStore
	mu        sync.Mutex
	puts      map[string]int
	getCounts map[string]int
	deletes   map[string]int
}

func newCountingBlobStore(next BlobStore) *countingBlobStore {
	return &countingBlobStore{
		BlobStore: next,
		puts:      map[string]int{},
		getCounts: map[string]int{},
		deletes:   map[string]int{},
	}
}

func (c *countingBlobStore) Put(bucket, hash string, data []byte) error {
	c.mu.Lock()
	c.puts[bucket+"/"+hash]++
	c.mu.Unlock()
	return c.BlobStore.Put(bucket, hash, data)
}

func (c *countingBlobStore) Get(bucket, hash string) ([]byte, error) {
	c.mu.Lock()
	c.getCounts[bucket+"/"+hash]++
	c.mu.Unlock()
	return c.BlobStore.Get(bucket, hash)
}

func (c *countingBlobStore) gets() map[string]int {
	return c.getCounts
}

func (c *countingBlobStore) Delete(bucket, hash string) error {
	c.mu.Lock()
	c.deletes[bucket+"/"+hash]++
	c.mu.Unlock()
	return c.BlobStore.Delete(bucket, hash)
}

func (c *countingBlobStore) count(m map[string]int, bucket, hash string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return m[bucket+"/"+hash]
}

func TestPutGetDeleteRoundTrip(t *testing.T) {
	for _, tc := range backendTestCases() {
		t.Run(tc.name, func(t *testing.T) {
			s := tc.newStore(t)

			if err := s.Put("b1", "obj1", []byte("hello")); err != nil {
				t.Fatalf("Put: %v", err)
			}

			data, err := s.Get("b1", "obj1")
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			if string(data) != "hello" {
				t.Errorf("Get data = %q, want %q", data, "hello")
			}

			if err := s.Delete("b1", "obj1"); err != nil {
				t.Fatalf("Delete: %v", err)
			}

			if _, err := s.Get("b1", "obj1"); !errors.Is(err, ErrNotFound) {
				t.Errorf("Get after delete: err = %v, want ErrNotFound", err)
			}
			if err := s.Delete("b1", "obj1"); !errors.Is(err, ErrNotFound) {
				t.Errorf("double Delete: err = %v, want ErrNotFound", err)
			}
		})
	}
}

func TestGetDeleteMissingReturnsNotFound(t *testing.T) {
	for _, tc := range backendTestCases() {
		t.Run(tc.name, func(t *testing.T) {
			s := tc.newStore(t)
			if _, err := s.Get("nope", "nope"); !errors.Is(err, ErrNotFound) {
				t.Errorf("Get: err = %v, want ErrNotFound", err)
			}
			if err := s.Delete("nope", "nope"); !errors.Is(err, ErrNotFound) {
				t.Errorf("Delete: err = %v, want ErrNotFound", err)
			}
		})
	}
}

func TestInvalidNamesRejected(t *testing.T) {
	for _, tc := range backendTestCases() {
		t.Run(tc.name, func(t *testing.T) {
			s := tc.newStore(t)
			bad := []string{"", ".", "..", "a/b"}
			for _, name := range bad {
				if err := s.Put(name, "obj", []byte("x")); !errors.Is(err, ErrInvalidName) {
					t.Errorf("Put(bucket=%q): err = %v, want ErrInvalidName", name, err)
				}
				if err := s.Put("bucket", name, []byte("x")); !errors.Is(err, ErrInvalidName) {
					t.Errorf("Put(objectID=%q): err = %v, want ErrInvalidName", name, err)
				}
			}
		})
	}
}

func TestDedupSharesBlobAndTracksRefcount(t *testing.T) {
	for _, tc := range backendTestCases() {
		t.Run(tc.name, func(t *testing.T) {
			blobs, persist := tc.rawParts(t)
			counting := newCountingBlobStore(blobs)
			s := NewObjectStore(counting, persist)

			content := []byte("shared content")
			hash := hashBytes(content)

			if err := s.Put("b1", "obj1", content); err != nil {
				t.Fatalf("Put obj1: %v", err)
			}
			if err := s.Put("b1", "obj2", content); err != nil {
				t.Fatalf("Put obj2: %v", err)
			}

			if got := counting.count(counting.puts, "b1", hash); got != 1 {
				t.Errorf("blob Put called %d times, want 1 (dedup should skip the second write)", got)
			}

			data1, err := s.Get("b1", "obj1")
			if err != nil || string(data1) != string(content) {
				t.Errorf("Get obj1 = (%q, %v), want (%q, nil)", data1, err, content)
			}
			data2, err := s.Get("b1", "obj2")
			if err != nil || string(data2) != string(content) {
				t.Errorf("Get obj2 = (%q, %v), want (%q, nil)", data2, err, content)
			}

			if err := s.Delete("b1", "obj1"); err != nil {
				t.Fatalf("Delete obj1: %v", err)
			}
			if got := counting.count(counting.deletes, "b1", hash); got != 0 {
				t.Errorf("blob Delete called %d times after first objectID removed, want 0 (obj2 still references it)", got)
			}
			if _, err := s.Get("b1", "obj2"); err != nil {
				t.Errorf("Get obj2 after deleting obj1: %v, want nil (still referenced)", err)
			}

			if err := s.Delete("b1", "obj2"); err != nil {
				t.Fatalf("Delete obj2: %v", err)
			}
			if got := counting.count(counting.deletes, "b1", hash); got != 1 {
				t.Errorf("blob Delete called %d times after both objectIDs removed, want 1", got)
			}
		})
	}
}

func TestOverwriteReleasesOldBlobWhenUnreferenced(t *testing.T) {
	for _, tc := range backendTestCases() {
		t.Run(tc.name, func(t *testing.T) {
			blobs, persist := tc.rawParts(t)
			counting := newCountingBlobStore(blobs)
			s := NewObjectStore(counting, persist)

			oldContent := []byte("old")
			newContent := []byte("new")
			oldHash := hashBytes(oldContent)

			if err := s.Put("b1", "obj1", oldContent); err != nil {
				t.Fatalf("Put old: %v", err)
			}
			if err := s.Put("b1", "obj1", newContent); err != nil {
				t.Fatalf("Put new: %v", err)
			}

			if got := counting.count(counting.deletes, "b1", oldHash); got != 1 {
				t.Errorf("old blob Delete called %d times, want 1 (no longer referenced)", got)
			}

			data, err := s.Get("b1", "obj1")
			if err != nil || string(data) != "new" {
				t.Errorf("Get after overwrite = (%q, %v), want (new, nil)", data, err)
			}
		})
	}
}

func TestBucketsAreIsolated(t *testing.T) {
	for _, tc := range backendTestCases() {
		t.Run(tc.name, func(t *testing.T) {
			s := tc.newStore(t)
			if err := s.Put("b1", "obj", []byte("in b1")); err != nil {
				t.Fatalf("Put b1: %v", err)
			}
			if _, err := s.Get("b2", "obj"); !errors.Is(err, ErrNotFound) {
				t.Errorf("Get b2/obj: err = %v, want ErrNotFound (buckets must be isolated)", err)
			}
		})
	}
}

func TestConcurrentAccessAcrossBucketsIsRaceFree(t *testing.T) {
	s := NewObjectStore(NewMemoryBlobStore(), nil)
	const buckets = 8
	const perBucket = 50

	var wg sync.WaitGroup
	for b := range buckets {
		bucket := fmt.Sprintf("bucket-%d", b)
		wg.Add(1)
		go func(bucket string) {
			defer wg.Done()
			for i := range perBucket {
				objectID := fmt.Sprintf("obj-%d", i)
				data := fmt.Appendf(nil, "data-%s-%d", bucket, i)
				if err := s.Put(bucket, objectID, data); err != nil {
					t.Errorf("Put: %v", err)
					return
				}
				if _, err := s.Get(bucket, objectID); err != nil {
					t.Errorf("Get: %v", err)
					return
				}
				if err := s.Delete(bucket, objectID); err != nil {
					t.Errorf("Delete: %v", err)
					return
				}
			}
		}(bucket)
	}
	wg.Wait()
}

func TestConcurrentAccessAcrossBucketsDiskBackendIsRaceFree(t *testing.T) {
	// Same shape as the memory-backend version above, but exercises
	// DiskBlobStore and JSONIndexPersister concurrently across many
	// buckets - the two components that must not introduce shared mutable
	// state of their own now that ObjectStore's per-bucket locking is the
	// only thing serializing access to them (see IndexPersister's doc
	// comment in engine.go).
	dir := t.TempDir()
	s := NewObjectStore(NewDiskBlobStore(dir), NewJSONIndexPersister(dir))
	const buckets = 8
	const perBucket = 25

	var wg sync.WaitGroup
	for b := range buckets {
		bucket := fmt.Sprintf("bucket-%d", b)
		wg.Add(1)
		go func(bucket string) {
			defer wg.Done()
			for i := range perBucket {
				objectID := fmt.Sprintf("obj-%d", i)
				data := fmt.Appendf(nil, "data-%s-%d", bucket, i)
				if err := s.Put(bucket, objectID, data); err != nil {
					t.Errorf("Put: %v", err)
					return
				}
				if got, err := s.Get(bucket, objectID); err != nil || string(got) != string(data) {
					t.Errorf("Get = (%q, %v), want (%q, nil)", got, err, data)
					return
				}
				if err := s.Delete(bucket, objectID); err != nil {
					t.Errorf("Delete: %v", err)
					return
				}
			}
		}(bucket)
	}
	wg.Wait()
}

func TestConcurrentFirstAccessToSameBucketConverges(t *testing.T) {
	// Regression test for the getBucket fast path: many goroutines racing to
	// create the *same* brand-new bucket for the first time must all end up
	// sharing one bucketState, not each getting their own (which would
	// silently lose writes depending on scheduling).
	s := NewObjectStore(NewMemoryBlobStore(), nil)
	const n = 50

	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			objectID := fmt.Sprintf("obj-%d", i)
			if err := s.Put("shared-bucket", objectID, []byte("x")); err != nil {
				t.Errorf("Put: %v", err)
			}
		}(i)
	}
	wg.Wait()

	for i := range n {
		objectID := fmt.Sprintf("obj-%d", i)
		if _, err := s.Get("shared-bucket", objectID); err != nil {
			t.Errorf("Get %s: %v (write was lost - bucket state split across goroutines)", objectID, err)
		}
	}
}

func TestDiskBackendPersistsAcrossRestart(t *testing.T) {
	dir := t.TempDir()

	blobs1 := NewDiskBlobStore(dir)
	persist1 := NewJSONIndexPersister(dir)
	s1 := NewObjectStore(blobs1, persist1)

	if err := s1.Put("b1", "obj1", []byte("durable")); err != nil {
		t.Fatalf("Put: %v", err)
	}

	// Simulate a restart: brand new ObjectStore, same data directory, no
	// shared in-memory state.
	blobs2 := NewDiskBlobStore(dir)
	persist2 := NewJSONIndexPersister(dir)
	s2 := NewObjectStore(blobs2, persist2)

	data, err := s2.Get("b1", "obj1")
	if err != nil {
		t.Fatalf("Get after restart: %v", err)
	}
	if string(data) != "durable" {
		t.Errorf("Get after restart = %q, want durable", data)
	}
}

// --- test scaffolding shared across the table-driven tests above ---

type backendTestCase struct {
	name     string
	newStore func(t *testing.T) *ObjectStore
	rawParts func(t *testing.T) (BlobStore, IndexPersister)
}

func backendTestCases() []backendTestCase {
	return []backendTestCase{
		{
			name: "memory",
			newStore: func(t *testing.T) *ObjectStore {
				return NewObjectStore(NewMemoryBlobStore(), nil)
			},
			rawParts: func(t *testing.T) (BlobStore, IndexPersister) {
				return NewMemoryBlobStore(), nil
			},
		},
		{
			name: "disk",
			newStore: func(t *testing.T) *ObjectStore {
				dir := t.TempDir()
				return NewObjectStore(NewDiskBlobStore(dir), NewJSONIndexPersister(dir))
			},
			rawParts: func(t *testing.T) (BlobStore, IndexPersister) {
				dir := t.TempDir()
				return NewDiskBlobStore(dir), NewJSONIndexPersister(dir)
			},
		},
	}
}
