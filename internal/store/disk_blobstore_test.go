package store

import (
	"errors"
	"testing"
)

func TestDiskBlobStorePutGetDelete(t *testing.T) {
	d := NewDiskBlobStore(t.TempDir())

	if err := d.Put("b1", "h1", []byte("hello")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	data, err := d.Get("b1", "h1")
	if err != nil || string(data) != "hello" {
		t.Fatalf("Get = (%q, %v), want (hello, nil)", data, err)
	}
	if err := d.Delete("b1", "h1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := d.Get("b1", "h1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get after Delete: err = %v, want ErrNotFound", err)
	}
}

func TestDiskBlobStoreGetMissingReturnsNotFound(t *testing.T) {
	d := NewDiskBlobStore(t.TempDir())
	if _, err := d.Get("b1", "never-written"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get: err = %v, want ErrNotFound", err)
	}
}

func TestDiskBlobStoreDeleteMissingIsNoOp(t *testing.T) {
	d := NewDiskBlobStore(t.TempDir())
	if err := d.Delete("b1", "never-written"); err != nil {
		t.Errorf("Delete: err = %v, want nil", err)
	}
}

func TestDiskBlobStoreRejectsUnsafeNames(t *testing.T) {
	d := NewDiskBlobStore(t.TempDir())
	unsafe := []string{"", ".", "..", "a/b", "a\\b"}

	for _, name := range unsafe {
		if err := d.Put(name, "h1", []byte("x")); !errors.Is(err, ErrInvalidName) {
			t.Errorf("Put(bucket=%q): err = %v, want ErrInvalidName", name, err)
		}
		if err := d.Put("b1", name, []byte("x")); !errors.Is(err, ErrInvalidName) {
			t.Errorf("Put(hash=%q): err = %v, want ErrInvalidName", name, err)
		}
		if _, err := d.Get("b1", name); !errors.Is(err, ErrInvalidName) {
			t.Errorf("Get(hash=%q): err = %v, want ErrInvalidName", name, err)
		}
		if err := d.Delete("b1", name); !errors.Is(err, ErrInvalidName) {
			t.Errorf("Delete(hash=%q): err = %v, want ErrInvalidName", name, err)
		}
	}
}
