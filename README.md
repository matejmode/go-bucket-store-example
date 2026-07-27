# go-bucket-store-example

An HTTP service that stores objects organized by buckets, addressed by
`/objects/{bucket}/{objectID}`, with `PUT` / `GET` / `DELETE`. Objects are
deduplicated within a bucket by content hash, and can be held in memory or
persisted to disk.

## Use of AI in this project

**Planning.** I first gave the assignment with detailed description how I think it could be built to Gemini to get an overview and a
first-pass implementation plan. I like to combine it with Claude as they sometimes have a nice different pattern suggestions. 
Resulting plan proposed `goleveldb` for disk persistence and `hashicorp/golang-lru` as a read cache. I brought that plan
into Claude Code to double-check that it makes sense.

**I rejected the first plan's storage design before any code was written.**
The LevelDB + LRU-library combination was Gemini's suggestion. I pushed back on it directly, asking whether the
simpler alternative (plain files) still counted as persistent, and whether
pulling in an embedded database was actually appropriate for what's
described. I picked plain files on disk instead. Also instead of using a third-party cache
library I tried to prompt Claude to generate LRU cache instead. This way result has zero third-party dependencies.

**Implementation.** Claude wrote the Go code directly (storage engine, HTTP
layer, config, tests, Dockerfile) following the design above. I reviewed the
diffs, ran `go build`/`go vet`/`go test -race` after each stage, and built +
ran the Docker image with `curl` tests (`PUT` → `GET` → `DELETE` →
verify `404`, plus a real process-restart test for disk-mode persistence)
to confirm the whole path actually works end to end, not just that it
compiles.

**I asked for two separate, later review passes** — a security pass and a
performance pass — specifically because the first pass is never
sufficient; I wanted a second, adversarial look at code that already
"worked." Each one found and fixed something real:

*Security pass:*
- Path traversal via `bucket`/`objectID` in disk file paths — mitigated by
  charset validation at both the HTTP layer and, as defense-in-depth, again
  inside the store package.
- **Second pass, run because I explicitly asked to "look for
  vulnerabilities with http requests"** rather than treat the first pass as done: at the
  time, GET echoed a client-supplied `Content-Type` with no
  `X-Content-Type-Options` header (a stored-XSS-shaped risk, same class as
  public S3 buckets serving arbitrary content types to browsers) — fixed
  first by adding `nosniff`; no `ReadHeaderTimeout` on the
  `http.Server` (slowloris-style exposure — fixed); and the Docker image
  running as root by default (fixed by switching to
  `gcr.io/distroless/static-debian12:nonroot`).

*Performance pass, run because I explicitly asked to look for optimizable
structures:*
- Bucket lookup was going through a single `sync.Mutex` shared by every
  request regardless of bucket — a real bottleneck that undercut the whole
  point of the per-bucket sharded locking design. Replaced with `sync.Map`.
- `PUT` was reading the body with `io.ReadAll`'s default geometric growth
  instead of pre-sizing from `Content-Length`. Fixed.
- A few other spots (single-mutex cache/blob-map, JSON response encoding)
  were reviewed and deliberately left as-is even tho Claude had some suggestions.

**A later round: deliberately raising test coverage, not just adding more
tests.** I asked for coverage to be checked and closed where it made sense.

**Content-Type support.** Claude tried to guide me to handle different Content-Types, 
but I have decided to keep it without them and treating everything as raw format.
Given the spec describes objects as simply the raw text of an HTTP request.

What I did *not* do: accept any of this on the first pass. The value here
wasn't asking Claude to write Go — it was directing *where* to look twice
(the storage design before writing code; security and performance after the
"working" version existed; coverage and a further round of hands-on testing
after that) and pushing back when an answer didn't hold up, rather than
treating "it compiles and the tests pass" as equivalent to "this is done."

Always an interesting experience to spend X hours debugging the code written mainly by AI rather than writing it alone. 
Nevertheless I think this result is production ready. The main drawback would be the header handling - I treat everything as raw *Content-Type*.
I also included instructions on how to run with podman for bonus points.  

---

## Quick start

```bash
# listens on :8080, in-memory storage
go run ./cmd/server

# listens on :8080, disk-backed storage
go run ./cmd/server -storage disk -data-dir ./data
```

```bash
curl -X PUT --data 'hello world' http://localhost:8080/objects/mybucket/obj1
# {"id":"obj1"}

curl http://localhost:8080/objects/mybucket/obj1
# hello world

curl -X DELETE http://localhost:8080/objects/mybucket/obj1
# (200 OK, empty body)
```

**Note:** `go run` compiles the code to a temp binary and runs it as a
separate child process. If stopping `go run` doesn't actually stop the
server (port still bound, terminal returned anyway), find and kill the
orphaned process directly, or run the built binary instead:

```bash
pgrep -fa 'cmd/server|/server -port'   # find the orphaned process
kill <pid>

# or avoid the wrapper entirely
go build -o server ./cmd/server && ./server
```

### Podman

```bash
podman build -t bucketstore .
podman run -p 8080:8080 bucketstore
# or, for disk-backed storage with a persistent volume:
podman run -p 8080:8080 -e STORAGE_BACKEND=disk -v bucketstore-data:/data bucketstore
```

Podman is typically rootless, so if you bind-mount a host directory instead
of a named volume for disk mode (`-v /host/path:/data` rather than
`-v bucketstore-data:/data`), that host path needs to already be writable by
the container's user (uid 65532 - see the Dockerfile's `:nonroot` base). A
named volume avoids this entirely, which is why the example above uses one.

## Configuration

Flags (with equivalent environment variables, flag wins if both are set):

| Flag | Env | Default | Description |
|---|---|---|---|
| `-port` | `PORT` | `8080` | port to listen on |
| `-storage` | `STORAGE_BACKEND` | `memory` | `memory` or `disk` |
| `-data-dir` | `DATA_DIR` | `./data` | storage directory (disk mode only) |
| `-cache-bytes` | `CACHE_BYTES` | `67108864` (64MiB) | read cache size in front of disk storage |
| `-max-body-bytes` | `MAX_BODY_BYTES` | `33554432` (32MiB) | max accepted request body size |

## API

| Method | Path | Success | Not found |
|---|---|---|---|
| `PUT` | `/objects/{bucket}/{objectID}` | `201 Created`, `{"id": "<objectID>"}` | — |
| `GET` | `/objects/{bucket}/{objectID}` | `200 OK`, object bytes | `404 Not Found` |
| `DELETE` | `/objects/{bucket}/{objectID}` | `200 OK` | `404 Not Found` |

Both `bucket` and `objectID` must match `^[A-Za-z0-9._-]{1,255}$` (and may not
be `.` or `..`); anything else returns `400 Bad Request`. PUT is an upsert:
re-PUTting an existing `objectID` overwrites it. Objects are stored and
served as opaque raw bytes: whatever `Content-Type` a client sends on `PUT`
is ignored, and `GET` always responds with `application/octet-stream`.

## Architecture

```
cmd/server/          wiring: config -> ObjectStore -> API -> http.Server, graceful shutdown
internal/config/     flag/env parsing
internal/api/         HTTP routing, validation, request logging, body-size limit
internal/store/       storage engine: dedup, refcounting, concurrency control
```

### Storage engine

One `ObjectStore` (`internal/store/engine.go`) implements dedup, refcounting,
and concurrency control identically for both backends. It's parametrized by
two small interfaces so the two modes share all of that logic instead of
duplicating it:

- **`BlobStore`** — persists content-addressed blob bytes for a `(bucket,
  hash)` key. `MemoryBlobStore` keeps them in a Go map; `DiskBlobStore` writes
  them as plain files under `{dataDir}/{bucket}/blobs/{hash}`, with an LRU
  read cache (`CachedBlobStore`) in front for the disk case.
- **`IndexPersister`** — durably stores each bucket's metadata (which
  `objectID`s map to which hash, and each hash's refcount) as one JSON file
  per bucket, written atomically (temp file + rename). `nil` in memory mode.
  This is what actually makes disk mode survive a restart: the blobs alone
  aren't enough, since the association from `objectID` to hash doesn't exist
  anywhere else. It's lazily loaded the first time a bucket is touched after
  startup, rather than scanning every bucket eagerly.

**Concurrency**: each bucket has its own `sync.RWMutex` (sharded, not global),
so writes to bucket A never block bucket B — only operations within the same
bucket serialize. Reads within a bucket use `RLock` and run concurrently with
each other. `MemoryBlobStore` has its own separate internal mutex, because
its backing map is shared across all buckets: even though different buckets
never touch the same key, concurrent access to different keys of the *same*
Go map from different goroutines is still a data race. `DiskBlobStore` needs
no such lock — distinct files at distinct paths are safe to touch
concurrently at the OS level, and writes to the *same* file only ever happen
from within the owning bucket's lock.

**Dedup**: `Put` hashes the request body (SHA-256) and looks it up in the
bucket's refcount table. If this is the first object in the bucket with that
hash, the blob is written once; every subsequent `objectID` with identical
bytes just adds a reference. Deleting an `objectID` decrements the refcount
and only deletes the underlying blob once nothing in the bucket references
it any more. Overwriting an `objectID` with different bytes releases its old
hash the same way.

## Testing

```bash
go test ./... -race

# coverage: per-package summary, then a function-level breakdown
go test ./... -coverprofile=coverage.out
go tool cover -func=coverage.out
go tool cover -html=coverage.out   # interactive, line-by-line
```

Covers: dedup/refcount correctness (including that a shared blob is written
and deleted exactly once, not once per referencing object), bucket isolation,
overwrite semantics, disk persistence across a simulated restart, the LRU
cache's eviction behavior, concurrent access across buckets under `-race`,
error-injection paths (blob writes/reads/persists failing), and the HTTP
layer's status codes (`201`/`200`/`404`/`400`/`413`/`500`) via
`net/http/httptest`.

Current coverage: 90.8% overall (`internal/config` 100%, `internal/store`
91.3%, `internal/api` 97.0%, `cmd/server` 71.1%).
