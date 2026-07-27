package store

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// writeFileAtomic writes data to a temp file in the same directory as path
// and renames it into place, so a crash mid-write never leaves a corrupt
// file at path.
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // no-op once renamed

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("rename temp file: %w", err)
	}
	return nil
}

// isSafeName reports whether name is safe to use as a single path segment:
// non-empty, contains no path separators, and isn't a "." or ".." traversal
// sentinel. Used as defense-in-depth for values that end up in file paths,
// even when already validated at the HTTP layer.
func isSafeName(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	if strings.ContainsAny(name, "/\\") {
		return false
	}
	return true
}
