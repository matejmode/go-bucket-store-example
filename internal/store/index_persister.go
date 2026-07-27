package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// JSONIndexPersister durably stores one bucket's metadata (objectID -> hash
// mapping and hash refcounts) as a single JSON file per bucket, written
// atomically (temp file + rename) on every mutation.
//
// This scales with the size of one bucket, not the whole store - unrelated
// buckets never contend on the same file. For very large buckets, rewriting
// the whole index on every write is the known scaling limit of this
// approach (see README); an append-only log with periodic compaction would
// remove it at the cost of real added complexity that isn't warranted here.
type JSONIndexPersister struct {
	dataDir string
}

func NewJSONIndexPersister(dataDir string) *JSONIndexPersister {
	return &JSONIndexPersister{dataDir: dataDir}
}

// indexPath returns the path to bucket's index.json, or ErrInvalidName if
// bucket isn't safe to use as a path segment.
func (p *JSONIndexPersister) indexPath(bucket string) (string, error) {
	if !isSafeName(bucket) {
		return "", ErrInvalidName
	}
	return filepath.Join(p.dataDir, bucket, "index.json"), nil
}

// Load reads bucket's persisted metadata. found is false, with a nil
// error, if the bucket has never been saved before.
func (p *JSONIndexPersister) Load(bucket string) (bucketSnapshot, bool, error) {
	path, err := p.indexPath(bucket)
	if err != nil {
		return bucketSnapshot{}, false, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return bucketSnapshot{}, false, nil
		}
		return bucketSnapshot{}, false, fmt.Errorf("read index: %w", err)
	}
	var snap bucketSnapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		return bucketSnapshot{}, false, fmt.Errorf("parse index: %w", err)
	}
	if snap.Objects == nil {
		snap.Objects = make(map[string]string)
	}
	if snap.Refs == nil {
		snap.Refs = make(map[string]int)
	}
	return snap, true, nil
}

func (p *JSONIndexPersister) Save(bucket string, snap bucketSnapshot) error {
	path, err := p.indexPath(bucket)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create bucket dir: %w", err)
	}
	raw, err := json.Marshal(snap)
	if err != nil {
		return fmt.Errorf("encode index: %w", err)
	}
	return writeFileAtomic(path, raw)
}
