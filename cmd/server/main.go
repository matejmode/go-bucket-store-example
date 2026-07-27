// Command server runs the bucket object store HTTP service.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"bucketstore/internal/api"
	"bucketstore/internal/config"
	"bucketstore/internal/store"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// run parses args into a config, builds the object store and HTTP server,
// and blocks until either the server fails or a SIGINT/SIGTERM arrives - in
// the latter case it shuts down gracefully, letting in-flight requests
// finish before returning.
func run(args []string) error {
	cfg, err := config.Load(args)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	objStore, err := buildObjectStore(cfg, logger)
	if err != nil {
		return fmt.Errorf("build object store: %w", err)
	}

	a := api.New(objStore, logger, cfg.MaxBodyBytes)

	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.Port),
		Handler: a.Routes(),
		// ReadHeaderTimeout bounds how long a client can dribble in request
		// headers before being dropped - without it, ReadTimeout alone still
		// leaves the connection-accept path open to slow-header exhaustion
		// (a slowloris-style attack) since it only bounds the full request.
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	serveErr := make(chan error, 1)
	go func() {
		logger.Info("listening", "addr", srv.Addr, "storage_backend", cfg.Backend)
		serveErr <- srv.ListenAndServe()
	}()

	select {
	case err := <-serveErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve: %w", err)
		}
		return nil
	case <-ctx.Done():
		logger.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("graceful shutdown: %w", err)
		}
		return nil
	}
}

// buildObjectStore constructs the ObjectStore for cfg.Backend: an
// unpersisted MemoryBlobStore for "memory", or, for "disk", a
// DiskBlobStore wrapped in an LRU read cache plus a JSONIndexPersister so
// bucket metadata survives a restart (creating cfg.DataDir if needed).
func buildObjectStore(cfg config.Config, logger *slog.Logger) (*store.ObjectStore, error) {
	switch cfg.Backend {
	case config.BackendMemory:
		return store.NewObjectStore(store.NewMemoryBlobStore(), nil), nil

	case config.BackendDisk:
		if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
			return nil, fmt.Errorf("create data dir: %w", err)
		}
		disk := store.NewDiskBlobStore(cfg.DataDir)
		cached := store.NewCachedBlobStore(disk, cfg.CacheBytes)
		persister := store.NewJSONIndexPersister(cfg.DataDir)
		logger.Info("disk storage configured", "data_dir", cfg.DataDir, "cache_bytes", cfg.CacheBytes)
		return store.NewObjectStore(cached, persister), nil

	default:
		return nil, fmt.Errorf("unknown storage backend %q", cfg.Backend)
	}
}
