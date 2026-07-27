package store

import (
	"errors"
	"testing"
)

func TestMemoryBlobStorePutGetDelete(t *testing.T) {
	m := NewMemoryBlobStore()

	if err := m.Put("b1", "h1", []byte("hello")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	data, err := m.Get("b1", "h1")
	if err != nil || string(data) != "hello" {
		t.Fatalf("Get = (%q, %v), want (hello, nil)", data, err)
	}
	if err := m.Delete("b1", "h1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := m.Get("b1", "h1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get after Delete: err = %v, want ErrNotFound", err)
	}
}

func TestMemoryBlobStoreGetMissingBucketReturnsNotFound(t *testing.T) {
	m := NewMemoryBlobStore()
	if _, err := m.Get("never-created", "h1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get on missing bucket: err = %v, want ErrNotFound", err)
	}
}

func TestMemoryBlobStoreGetMissingHashReturnsNotFound(t *testing.T) {
	m := NewMemoryBlobStore()
	if err := m.Put("b1", "h1", []byte("x")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if _, err := m.Get("b1", "h-other"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get on missing hash: err = %v, want ErrNotFound", err)
	}
}

func TestMemoryBlobStoreDeleteMissingBucketIsNoOp(t *testing.T) {
	m := NewMemoryBlobStore()
	if err := m.Delete("never-created", "h1"); err != nil {
		t.Errorf("Delete on missing bucket: err = %v, want nil", err)
	}
}

func TestMemoryBlobStorePutCopiesInput(t *testing.T) {
	m := NewMemoryBlobStore()
	original := []byte("hello")
	if err := m.Put("b1", "h1", original); err != nil {
		t.Fatalf("Put: %v", err)
	}
	original[0] = 'X' // mutate the caller's slice after Put

	data, err := m.Get("b1", "h1")
	if err != nil || string(data) != "hello" {
		t.Errorf("Get = (%q, %v), want (hello, nil) - stored data must not alias the caller's slice", data, err)
	}
}

func TestMemoryBlobStoreGetReturnsCopyNotAlias(t *testing.T) {
	m := NewMemoryBlobStore()
	if err := m.Put("b1", "h1", []byte("hello")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	data, err := m.Get("b1", "h1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	data[0] = 'X' // mutate the returned slice

	data2, err := m.Get("b1", "h1")
	if err != nil || string(data2) != "hello" {
		t.Errorf("second Get = (%q, %v), want (hello, nil) - internal storage must not alias the returned slice", data2, err)
	}
}
