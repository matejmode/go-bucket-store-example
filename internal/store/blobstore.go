package store

// BlobStore persists content-addressed blobs, scoped by bucket and hash.
//
// Implementations are called exclusively while the owning bucket's lock is
// held by ObjectStore, so writes to a given (bucket, hash) key are always
// serialized by the caller. However, different buckets are accessed
// concurrently (that's the point of the sharded bucket locks), so an
// implementation backed by shared in-process state (like a Go map) must
// still guard that shared state itself - see MemoryBlobStore.
type BlobStore interface {
	Put(bucket, hash string, data []byte) error
	Get(bucket, hash string) ([]byte, error) // ErrNotFound if missing
	Delete(bucket, hash string) error
}
