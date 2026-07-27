package store

import "sync"

// MemoryBlobStore holds blob bytes entirely in RAM. Data does not survive a
// process restart.
//
// The map is shared across all buckets, so unlike ObjectStore's per-bucket
// locks (which allow different buckets to proceed fully in parallel),
// MemoryBlobStore needs its own mutex: Go maps are not safe for concurrent
// access even on disjoint keys. The critical sections here are trivial O(1)
// map operations, so this does not reintroduce the cross-bucket contention
// the sharded design is meant to avoid.
type MemoryBlobStore struct {
	mu   sync.RWMutex
	data map[string]map[string][]byte // bucket -> hash -> bytes
}

func NewMemoryBlobStore() *MemoryBlobStore {
	return &MemoryBlobStore{data: make(map[string]map[string][]byte)}
}

// Put stores a copy of data under (bucket, hash), overwriting any existing
// value at that key.
func (m *MemoryBlobStore) Put(bucket, hash string, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.data[bucket]
	if !ok {
		b = make(map[string][]byte)
		m.data[bucket] = b
	}
	cp := make([]byte, len(data))
	copy(cp, data)
	b[hash] = cp
	return nil
}

// Get returns a copy of the bytes stored at (bucket, hash), or ErrNotFound
// if the bucket or hash doesn't exist.
func (m *MemoryBlobStore) Get(bucket, hash string) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	b, ok := m.data[bucket]
	if !ok {
		return nil, ErrNotFound
	}
	data, ok := b[hash]
	if !ok {
		return nil, ErrNotFound
	}
	cp := make([]byte, len(data))
	copy(cp, data)
	return cp, nil
}

// Delete removes (bucket, hash) if present; deleting a missing key is a
// no-op, not an error.
func (m *MemoryBlobStore) Delete(bucket, hash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.data[bucket]
	if !ok {
		return nil
	}
	delete(b, hash)
	return nil
}
