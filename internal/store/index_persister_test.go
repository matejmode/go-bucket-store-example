package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestJSONIndexPersisterLoadNotFound(t *testing.T) {
	p := NewJSONIndexPersister(t.TempDir())
	_, found, err := p.Load("never-seen")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if found {
		t.Errorf("found = true, want false for a bucket never saved")
	}
}

func TestJSONIndexPersisterSaveLoadRoundTrip(t *testing.T) {
	p := NewJSONIndexPersister(t.TempDir())
	snap := bucketSnapshot{
		Objects: map[string]string{"obj1": "h1"},
		Refs:    map[string]int{"h1": 1},
	}
	if err := p.Save("b1", snap); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, found, err := p.Load("b1")
	if err != nil || !found {
		t.Fatalf("Load = (%v, %v, %v), want (_, true, nil)", got, found, err)
	}
	if got.Objects["obj1"] != snap.Objects["obj1"] || got.Refs["h1"] != 1 {
		t.Errorf("Load = %+v, want %+v", got, snap)
	}
}

func TestJSONIndexPersisterLoadMalformedJSONReturnsError(t *testing.T) {
	dir := t.TempDir()
	bucketDir := filepath.Join(dir, "b1")
	if err := os.MkdirAll(bucketDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(bucketDir, "index.json"), []byte("not json"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	p := NewJSONIndexPersister(dir)
	if _, _, err := p.Load("b1"); err == nil {
		t.Errorf("Load: err = nil, want a parse error for malformed JSON")
	}
}

func TestJSONIndexPersisterRejectsUnsafeBucketNames(t *testing.T) {
	p := NewJSONIndexPersister(t.TempDir())
	unsafe := []string{"", ".", "..", "a/b"}
	for _, name := range unsafe {
		if _, _, err := p.Load(name); !errors.Is(err, ErrInvalidName) {
			t.Errorf("Load(%q): err = %v, want ErrInvalidName", name, err)
		}
		if err := p.Save(name, bucketSnapshot{}); !errors.Is(err, ErrInvalidName) {
			t.Errorf("Save(%q): err = %v, want ErrInvalidName", name, err)
		}
	}
}
