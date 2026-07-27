package store

import (
	"fmt"
	"os"
	"path/filepath"
)

// DiskBlobStore persists blob bytes as plain files under dataDir, one
// directory per bucket. Blobs are content-addressed by hash, so a given
// (bucket, hash) file is only ever written once (ObjectStore only calls Put
// when the refcount transitions from zero) and concurrent access to
// distinct files at distinct paths is safe at the OS level without any
// additional in-process locking here.
type DiskBlobStore struct {
	dataDir string
}

func NewDiskBlobStore(dataDir string) *DiskBlobStore {
	return &DiskBlobStore{dataDir: dataDir}
}

// blobPath returns the on-disk path for (bucket, hash), or ErrInvalidName
// if either isn't safe to use as a path segment.
func (d *DiskBlobStore) blobPath(bucket, hash string) (string, error) {
	if !isSafeName(bucket) || !isSafeName(hash) {
		return "", ErrInvalidName
	}
	return filepath.Join(d.dataDir, bucket, "blobs", hash), nil
}

// Put atomically writes data to the file for (bucket, hash), creating any
// missing parent directories first.
func (d *DiskBlobStore) Put(bucket, hash string, data []byte) error {
	path, err := d.blobPath(bucket, hash)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create blob dir: %w", err)
	}
	return writeFileAtomic(path, data)
}

// Get reads the file for (bucket, hash), returning ErrNotFound if it
// doesn't exist.
func (d *DiskBlobStore) Get(bucket, hash string) ([]byte, error) {
	path, err := d.blobPath(bucket, hash)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("read blob: %w", err)
	}
	return data, nil
}

// Delete removes the file for (bucket, hash); deleting a missing file is a
// no-op, not an error.
func (d *DiskBlobStore) Delete(bucket, hash string) error {
	path, err := d.blobPath(bucket, hash)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete blob: %w", err)
	}
	return nil
}
