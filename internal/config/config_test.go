package config

import "testing"

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load(nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Port != 8080 {
		t.Errorf("Port = %d, want 8080", cfg.Port)
	}
	if cfg.Backend != BackendMemory {
		t.Errorf("Backend = %q, want %q", cfg.Backend, BackendMemory)
	}
	if cfg.DataDir != "./data" {
		t.Errorf("DataDir = %q, want ./data", cfg.DataDir)
	}
	if cfg.CacheBytes != 64<<20 {
		t.Errorf("CacheBytes = %d, want %d", cfg.CacheBytes, 64<<20)
	}
	if cfg.MaxBodyBytes != 32<<20 {
		t.Errorf("MaxBodyBytes = %d, want %d", cfg.MaxBodyBytes, 32<<20)
	}
}

func TestLoadFlagsOverrideDefaults(t *testing.T) {
	cfg, err := Load([]string{"-port", "9090", "-storage", "disk", "-data-dir", "/tmp/x", "-cache-bytes", "123", "-max-body-bytes", "456"})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Port != 9090 {
		t.Errorf("Port = %d, want 9090", cfg.Port)
	}
	if cfg.Backend != BackendDisk {
		t.Errorf("Backend = %q, want %q", cfg.Backend, BackendDisk)
	}
	if cfg.DataDir != "/tmp/x" {
		t.Errorf("DataDir = %q, want /tmp/x", cfg.DataDir)
	}
	if cfg.CacheBytes != 123 {
		t.Errorf("CacheBytes = %d, want 123", cfg.CacheBytes)
	}
	if cfg.MaxBodyBytes != 456 {
		t.Errorf("MaxBodyBytes = %d, want 456", cfg.MaxBodyBytes)
	}
}

func TestLoadEnvUsedWhenNoFlag(t *testing.T) {
	t.Setenv("PORT", "9191")
	t.Setenv("STORAGE_BACKEND", "disk")
	t.Setenv("DATA_DIR", "/env/data")
	t.Setenv("CACHE_BYTES", "999")
	t.Setenv("MAX_BODY_BYTES", "888")

	cfg, err := Load(nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Port != 9191 {
		t.Errorf("Port = %d, want 9191", cfg.Port)
	}
	if cfg.Backend != BackendDisk {
		t.Errorf("Backend = %q, want %q", cfg.Backend, BackendDisk)
	}
	if cfg.DataDir != "/env/data" {
		t.Errorf("DataDir = %q, want /env/data", cfg.DataDir)
	}
	if cfg.CacheBytes != 999 {
		t.Errorf("CacheBytes = %d, want 999", cfg.CacheBytes)
	}
	if cfg.MaxBodyBytes != 888 {
		t.Errorf("MaxBodyBytes = %d, want 888", cfg.MaxBodyBytes)
	}
}

func TestLoadFlagOverridesEnv(t *testing.T) {
	t.Setenv("PORT", "9191")
	cfg, err := Load([]string{"-port", "9292"})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Port != 9292 {
		t.Errorf("Port = %d, want 9292 (flag should win over env)", cfg.Port)
	}
}

func TestLoadMalformedEnvFallsBackToDefault(t *testing.T) {
	t.Setenv("PORT", "not-a-number")
	t.Setenv("CACHE_BYTES", "also-not-a-number")

	cfg, err := Load(nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Port != 8080 {
		t.Errorf("Port = %d, want 8080 (malformed env should fall back to default)", cfg.Port)
	}
	if cfg.CacheBytes != 64<<20 {
		t.Errorf("CacheBytes = %d, want default (malformed env should fall back)", cfg.CacheBytes)
	}
}

func TestLoadRejectsInvalidBackend(t *testing.T) {
	if _, err := Load([]string{"-storage", "s3"}); err == nil {
		t.Error("Load: err = nil, want an error for an unknown storage backend")
	}
}

func TestLoadRejectsInvalidPort(t *testing.T) {
	cases := []string{"0", "-1", "65536", "100000"}
	for _, p := range cases {
		if _, err := Load([]string{"-port", p}); err == nil {
			t.Errorf("Load(-port %s): err = nil, want an error", p)
		}
	}
}

func TestLoadRejectsUnknownFlag(t *testing.T) {
	if _, err := Load([]string{"-not-a-real-flag", "x"}); err == nil {
		t.Error("Load: err = nil, want an error for an unrecognized flag")
	}
}
