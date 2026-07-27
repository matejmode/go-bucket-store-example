package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteFileAtomicRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f.txt")
	if err := writeFileAtomic(path, []byte("hello")); err != nil {
		t.Fatalf("writeFileAtomic: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "hello" {
		t.Fatalf("ReadFile = (%q, %v), want (hello, nil)", got, err)
	}
}

func TestWriteFileAtomicOverwritesExisting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f.txt")
	if err := writeFileAtomic(path, []byte("first")); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if err := writeFileAtomic(path, []byte("second")); err != nil {
		t.Fatalf("second write: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "second" {
		t.Fatalf("ReadFile = (%q, %v), want (second, nil)", got, err)
	}
}

func TestWriteFileAtomicNonexistentDirReturnsError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing-subdir", "f.txt")
	if err := writeFileAtomic(path, []byte("x")); err == nil {
		t.Errorf("writeFileAtomic into a nonexistent directory: err = nil, want an error")
	}
}

func TestIsSafeName(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"bucket1", true},
		{"a.b-c_d", true},
		{"", false},
		{".", false},
		{"..", false},
		{"a/b", false},
		{"a\\b", false},
		{"...", true}, // not exactly "." or "..", and contains no separators
	}
	for _, tc := range cases {
		if got := isSafeName(tc.name); got != tc.want {
			t.Errorf("isSafeName(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}
}
