package store

import (
	"errors"
	"testing"
)

// faultyBlobStore wraps a real BlobStore and lets a test force any of its
// three methods to fail, to exercise ObjectStore's error-propagation paths
// without needing to actually break the filesystem.
type faultyBlobStore struct {
	next                         BlobStore
	failPut, failGet, failDelete error
}

func (f *faultyBlobStore) Put(bucket, hash string, data []byte) error {
	if f.failPut != nil {
		return f.failPut
	}
	return f.next.Put(bucket, hash, data)
}

func (f *faultyBlobStore) Get(bucket, hash string) ([]byte, error) {
	if f.failGet != nil {
		return nil, f.failGet
	}
	return f.next.Get(bucket, hash)
}

func (f *faultyBlobStore) Delete(bucket, hash string) error {
	if f.failDelete != nil {
		return f.failDelete
	}
	return f.next.Delete(bucket, hash)
}

// faultyIndexPersister wraps a real IndexPersister (or nil, behaving like
// memory mode) and lets a test force Load/Save to fail on demand.
type faultyIndexPersister struct {
	next     IndexPersister
	failLoad error
	failSave error
}

func (f *faultyIndexPersister) Load(bucket string) (bucketSnapshot, bool, error) {
	if f.failLoad != nil {
		return bucketSnapshot{}, false, f.failLoad
	}
	if f.next != nil {
		return f.next.Load(bucket)
	}
	return bucketSnapshot{}, false, nil
}

func (f *faultyIndexPersister) Save(bucket string, snap bucketSnapshot) error {
	if f.failSave != nil {
		return f.failSave
	}
	if f.next != nil {
		return f.next.Save(bucket, snap)
	}
	return nil
}

var errBoom = errors.New("boom")

func TestPutBlobWriteErrorLeavesNoTrace(t *testing.T) {
	fb := &faultyBlobStore{next: NewMemoryBlobStore(), failPut: errBoom}
	s := NewObjectStore(fb, nil)

	if err := s.Put("b1", "obj1", []byte("x")); !errors.Is(err, errBoom) {
		t.Fatalf("Put err = %v, want wrapping %v", err, errBoom)
	}
	if _, err := s.Get("b1", "obj1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get after failed Put: err = %v, want ErrNotFound (no partial state)", err)
	}
}

func TestGetBlobReadErrorPropagates(t *testing.T) {
	fb := &faultyBlobStore{next: NewMemoryBlobStore()}
	s := NewObjectStore(fb, nil)

	if err := s.Put("b1", "obj1", []byte("x")); err != nil {
		t.Fatalf("Put: %v", err)
	}

	fb.failGet = errBoom
	if _, err := s.Get("b1", "obj1"); !errors.Is(err, errBoom) {
		t.Errorf("Get err = %v, want wrapping %v", err, errBoom)
	}
}

func TestDeleteBlobDeleteErrorPropagates(t *testing.T) {
	fb := &faultyBlobStore{next: NewMemoryBlobStore()}
	s := NewObjectStore(fb, nil)

	if err := s.Put("b1", "obj1", []byte("x")); err != nil {
		t.Fatalf("Put: %v", err)
	}

	fb.failDelete = errBoom
	if err := s.Delete("b1", "obj1"); !errors.Is(err, errBoom) {
		t.Errorf("Delete err = %v, want wrapping %v", err, errBoom)
	}

	// The objectID is removed from the index before the underlying blob
	// delete is attempted, so it's already gone from this bucket's point of
	// view even though the blob itself failed to delete (an orphaned blob,
	// not a correctness issue for reads/writes going forward).
	if _, err := s.Get("b1", "obj1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get after failed Delete: err = %v, want ErrNotFound", err)
	}
}

func TestGetBucketPersistLoadErrorPropagates(t *testing.T) {
	fip := &faultyIndexPersister{failLoad: errBoom}
	s := NewObjectStore(NewMemoryBlobStore(), fip)

	if err := s.Put("never-seen-bucket", "obj1", []byte("x")); !errors.Is(err, errBoom) {
		t.Errorf("Put err = %v, want wrapping %v", err, errBoom)
	}
	if _, err := s.Get("never-seen-bucket", "obj1"); !errors.Is(err, errBoom) {
		t.Errorf("Get err = %v, want wrapping %v", err, errBoom)
	}
}

func TestPutIndexPersistErrorPropagatesButStateAlreadyMutated(t *testing.T) {
	// Documents a known, accepted limitation (see README): the blob write
	// and the index persist are not one atomic transaction. If the persist
	// fails, Put reports an error, but the in-memory state (and the blob
	// itself) have already been updated - a subsequent read in the same
	// process succeeds despite the reported error.
	fip := &faultyIndexPersister{failSave: errBoom}
	s := NewObjectStore(NewMemoryBlobStore(), fip)

	err := s.Put("b1", "obj1", []byte("x"))
	if !errors.Is(err, errBoom) {
		t.Fatalf("Put err = %v, want wrapping %v", err, errBoom)
	}

	data, getErr := s.Get("b1", "obj1")
	if getErr != nil || string(data) != "x" {
		t.Errorf("Get after failed persist = (%q, %v), want (x, nil) - in-memory state should already reflect the write", data, getErr)
	}
}

func TestPutIdempotentSameContentSkipsPersist(t *testing.T) {
	dir := t.TempDir()
	fip := &faultyIndexPersister{next: NewJSONIndexPersister(dir)}
	s := NewObjectStore(NewDiskBlobStore(dir), fip)

	if err := s.Put("b1", "obj1", []byte("x")); err != nil {
		t.Fatalf("first Put: %v", err)
	}

	// Now make any further persist attempt fail. Re-putting identical bytes
	// must be a true no-op that doesn't touch the persister at all.
	fip.failSave = errBoom
	if err := s.Put("b1", "obj1", []byte("x")); err != nil {
		t.Errorf("idempotent re-Put err = %v, want nil (should skip persist entirely)", err)
	}
}
