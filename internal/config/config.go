// Package config parses server configuration from CLI flags, falling back
// to environment variables, then hardcoded defaults.
package config

import (
	"flag"
	"fmt"
	"os"
	"strconv"
)

const (
	BackendMemory = "memory"
	BackendDisk   = "disk"
)

type Config struct {
	Port         int
	Backend      string // BackendMemory or BackendDisk
	DataDir      string // only used when Backend == BackendDisk
	CacheBytes   int64  // LRU cache size in front of disk reads
	MaxBodyBytes int64  // max accepted request body size
}

// Load parses args (typically os.Args[1:]) into a Config. Flags take
// precedence over environment variables, which take precedence over
// defaults.
func Load(args []string) (Config, error) {
	fs := flag.NewFlagSet("bucketstore", flag.ContinueOnError)

	cfg := Config{}
	fs.IntVar(&cfg.Port, "port", envInt("PORT", 8080), "port to listen on")
	fs.StringVar(&cfg.Backend, "storage", envString("STORAGE_BACKEND", BackendMemory), "storage backend: memory or disk")
	fs.StringVar(&cfg.DataDir, "data-dir", envString("DATA_DIR", "./data"), "directory for on-disk storage (storage=disk only)")
	fs.Int64Var(&cfg.CacheBytes, "cache-bytes", envInt64("CACHE_BYTES", 64<<20), "max bytes held in the read cache in front of disk storage")
	fs.Int64Var(&cfg.MaxBodyBytes, "max-body-bytes", envInt64("MAX_BODY_BYTES", 32<<20), "max accepted request body size in bytes")

	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}

	if cfg.Backend != BackendMemory && cfg.Backend != BackendDisk {
		return Config{}, fmt.Errorf("invalid -storage %q: must be %q or %q", cfg.Backend, BackendMemory, BackendDisk)
	}
	if cfg.Port <= 0 || cfg.Port > 65535 {
		return Config{}, fmt.Errorf("invalid -port %d: must be between 1 and 65535", cfg.Port)
	}

	return cfg, nil
}

func envString(key, def string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v, ok := os.LookupEnv(key); ok {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envInt64(key string, def int64) int64 {
	if v, ok := os.LookupEnv(key); ok {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	return def
}
