package main

import (
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"bucketstore/internal/config"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestBuildObjectStoreMemory(t *testing.T) {
	cfg := config.Config{Backend: config.BackendMemory}
	s, err := buildObjectStore(cfg, discardLogger())
	if err != nil {
		t.Fatalf("buildObjectStore: %v", err)
	}
	if err := s.Put("b1", "obj1", []byte("x")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if _, err := s.Get("b1", "obj1"); err != nil {
		t.Fatalf("Get: %v", err)
	}
}

func TestBuildObjectStoreDiskCreatesDataDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "data")
	cfg := config.Config{Backend: config.BackendDisk, DataDir: dir, CacheBytes: 1 << 20}
	s, err := buildObjectStore(cfg, discardLogger())
	if err != nil {
		t.Fatalf("buildObjectStore: %v", err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("data dir was not created: %v", err)
	}
	if err := s.Put("b1", "obj1", []byte("x")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	data, err := s.Get("b1", "obj1")
	if err != nil || string(data) != "x" {
		t.Fatalf("Get = (%q, %v), want (x, nil)", data, err)
	}
}

func TestBuildObjectStoreDiskMkdirFailure(t *testing.T) {
	// Create a regular file, then ask for a data dir *under* that file -
	// MkdirAll must fail since a path component isn't a directory.
	blocker := filepath.Join(t.TempDir(), "im-a-file")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg := config.Config{Backend: config.BackendDisk, DataDir: filepath.Join(blocker, "data")}
	if _, err := buildObjectStore(cfg, discardLogger()); err == nil {
		t.Error("buildObjectStore: err = nil, want an error when the data dir can't be created")
	}
}

func TestBuildObjectStoreUnknownBackend(t *testing.T) {
	cfg := config.Config{Backend: "s3"}
	if _, err := buildObjectStore(cfg, discardLogger()); err == nil {
		t.Error("buildObjectStore: err = nil, want an error for an unknown backend")
	}
}

func TestRunReturnsConfigError(t *testing.T) {
	err := run([]string{"-storage", "not-a-real-backend"})
	if err == nil || !strings.Contains(err.Error(), "load config") {
		t.Errorf("run: err = %v, want a wrapped config-load error", err)
	}
}

func TestRunReturnsServeErrorOnPortConflict(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port

	err = run([]string{"-port", strconv.Itoa(port)})
	if err == nil || !strings.Contains(err.Error(), "serve") {
		t.Errorf("run: err = %v, want a wrapped serve error (port already in use)", err)
	}
}
