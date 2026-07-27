package store

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"sync"
)

// bucketSnapshot is the durable representation of one bucket's metadata:
// which objectIDs map to which content hash, and how many objects
// reference each hash, so a shared blob is deleted only once nothing
// references it any more.
type bucketSnapshot struct {
	Objects map[string]string `json:"objects"` // objectID -> hash
	Refs    map[string]int    `json:"refs"`    // hash -> refcount
}

type bucketState struct {
	mu   sync.RWMutex
	objs map[string]string // objectID -> hash
	refs map[string]int    // hash -> refcount
}

func newBucketState() *bucketState {
	return &bucketState{
		objs: make(map[string]string),
		refs: make(map[string]int),
	}
}

// snapshot copies out the bucket's metadata. Callers must hold b.mu.
func (b *bucketState) snapshot() bucketSnapshot {
	objs := make(map[string]string, len(b.objs))
	maps.Copy(objs, b.objs)
	refs := make(map[string]int, len(b.refs))
	maps.Copy(refs, b.refs)
	return bucketSnapshot{Objects: objs, Refs: refs}
}

// IndexPersister durably stores bucket metadata (the objectID -> hash
// mapping and hash refcounts) so a restart doesn't lose track of which
// objectIDs point at which blob, even though the blobs themselves remain on
// disk. Only used in disk-backed mode; memory mode passes a nil persister.
//
// Implementations don't need their own locking: ObjectStore only calls
// Save for a bucket while that bucket's RWMutex is held, so two Save calls
// for the same bucket never overlap, and Load is only ever called once per
// bucket per process - before that bucket's state (and thus any Save for
// it) exists. Different buckets are still saved/loaded fully concurrently,
// so an implementation must not introduce shared mutable state that would
// serialize across buckets (see JSONIndexPersister, which delegates
// entirely to the filesystem instead).
type IndexPersister interface {
	Load(bucket string) (snap bucketSnapshot, found bool, err error)
	Save(bucket string, snap bucketSnapshot) error
}

// ObjectStore is the dedup/refcount/concurrency core shared by both the
// in-memory and disk-backed configurations. It holds one bucketState per
// bucket, each guarded by its own RWMutex so unrelated buckets never block
// each other. Behavior differs only by which BlobStore and IndexPersister
// are injected.
type ObjectStore struct {
	blobs   BlobStore
	persist IndexPersister // nil => no durability beyond process lifetime

	// buckets maps bucket name -> *bucketState. sync.Map rather than a
	// plain map guarded by one mutex: every Put/Get/Delete call resolves its
	// bucket first, so a shared mutex here would be a hot, single point of
	// contention across every bucket on every request - exactly what the
	// per-bucket RWMutex sharding below is meant to avoid. sync.Map's
	// read path needs no lock once a bucket is resident (the common case:
	// buckets are created once, then looked up repeatedly).
	buckets sync.Map
}

func NewObjectStore(blobs BlobStore, persist IndexPersister) *ObjectStore {
	return &ObjectStore{
		blobs:   blobs,
		persist: persist,
	}
}

// getBucket returns the resident bucketState for name, hydrating it from
// the IndexPersister on first access (e.g. right after a restart) rather
// than requiring a full startup scan of every bucket.
func (s *ObjectStore) getBucket(name string) (*bucketState, error) {
	if v, ok := s.buckets.Load(name); ok {
		return v.(*bucketState), nil
	}

	b := newBucketState()
	if s.persist != nil {
		snap, found, err := s.persist.Load(name)
		if err != nil {
			return nil, fmt.Errorf("load bucket index: %w", err)
		}
		if found {
			b.objs = snap.Objects
			b.refs = snap.Refs
		}
	}

	actual, _ := s.buckets.LoadOrStore(name, b)
	return actual.(*bucketState), nil
}

// Put stores data under (bucket, objectID) as opaque raw bytes,
// deduplicating by SHA-256 of the body within the bucket. Re-putting
// identical bytes under a different objectID shares the same underlying
// blob; overwriting an existing objectID with different bytes releases the
// old blob (deleting it if no other objectID in the bucket still
// references it).
func (s *ObjectStore) Put(bucket, objectID string, data []byte) error {
	if !isSafeName(bucket) || !isSafeName(objectID) {
		return ErrInvalidName
	}
	b, err := s.getBucket(bucket)
	if err != nil {
		return err
	}
	hash := hashBytes(data)

	b.mu.Lock()
	defer b.mu.Unlock()

	oldHash, hadOld := b.objs[objectID]
	if hadOld && oldHash == hash {
		return nil
	}

	if b.refs[hash] == 0 {
		if err := s.blobs.Put(bucket, hash, data); err != nil {
			return fmt.Errorf("write blob: %w", err)
		}
	}
	b.refs[hash]++
	b.objs[objectID] = hash

	if hadOld {
		if err := s.releaseHashLocked(bucket, b, oldHash); err != nil {
			return err
		}
	}

	return s.persistLocked(bucket, b)
}

// Get returns the raw bytes stored at (bucket, objectID), or ErrNotFound if
// no such object exists.
func (s *ObjectStore) Get(bucket, objectID string) (data []byte, err error) {
	if !isSafeName(bucket) || !isSafeName(objectID) {
		return nil, ErrInvalidName
	}
	b, err := s.getBucket(bucket)
	if err != nil {
		return nil, err
	}

	b.mu.RLock()
	hash, ok := b.objs[objectID]
	b.mu.RUnlock()
	if !ok {
		return nil, ErrNotFound
	}

	return s.blobs.Get(bucket, hash)
}

// Delete removes (bucket, objectID), or returns ErrNotFound if it doesn't
// exist. The underlying blob is only deleted once no other objectID in the
// bucket still references it.
func (s *ObjectStore) Delete(bucket, objectID string) error {
	if !isSafeName(bucket) || !isSafeName(objectID) {
		return ErrInvalidName
	}
	b, err := s.getBucket(bucket)
	if err != nil {
		return err
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	hash, ok := b.objs[objectID]
	if !ok {
		return ErrNotFound
	}
	delete(b.objs, objectID)

	if err := s.releaseHashLocked(bucket, b, hash); err != nil {
		return err
	}
	return s.persistLocked(bucket, b)
}

// releaseHashLocked decrements hash's refcount and deletes the underlying
// blob once it reaches zero. Callers must hold b.mu.
func (s *ObjectStore) releaseHashLocked(bucket string, b *bucketState, hash string) error {
	b.refs[hash]--
	if b.refs[hash] <= 0 {
		delete(b.refs, hash)
		if err := s.blobs.Delete(bucket, hash); err != nil {
			return fmt.Errorf("delete blob: %w", err)
		}
	}
	return nil
}

// persistLocked writes b's metadata through to the IndexPersister, if any.
// Callers must hold b.mu.
func (s *ObjectStore) persistLocked(bucket string, b *bucketState) error {
	if s.persist == nil {
		return nil
	}
	if err := s.persist.Save(bucket, b.snapshot()); err != nil {
		return fmt.Errorf("persist bucket index: %w", err)
	}
	return nil
}

// hashBytes returns the hex-encoded SHA-256 digest of data, used as the
// content-addressed key that dedup is built on.
func hashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
