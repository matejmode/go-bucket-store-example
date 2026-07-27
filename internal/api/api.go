// Package api implements the HTTP delivery layer: routing, request
// validation, and translating store errors into HTTP status codes.
package api

import (
	"log/slog"
	"net/http"
	"regexp"

	"bucketstore/internal/store"
)

// nameRe restricts bucket names and object IDs to a safe, unambiguous
// charset. This is enforced here (yielding a clean 400) as well as
// defense-in-depth inside the store package, since both values end up in
// file paths when the disk backend is used.
var nameRe = regexp.MustCompile(`^[A-Za-z0-9._-]{1,255}$`)

// validName reports whether s is safe to use as a bucket name or object ID:
// non-empty, within the length limit, drawn only from the allowed charset,
// and not a literal "." or ".." traversal sentinel.
func validName(s string) bool {
	if s == "." || s == ".." {
		return false
	}
	return nameRe.MatchString(s)
}

type API struct {
	store        *store.ObjectStore
	logger       *slog.Logger
	maxBodyBytes int64
}

func New(s *store.ObjectStore, logger *slog.Logger, maxBodyBytes int64) *API {
	return &API{store: s, logger: logger, maxBodyBytes: maxBodyBytes}
}

// Routes builds the HTTP handler for the object store API, including
// request logging and a max body size limit.
func (a *API) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /objects/{bucket}/{objectID}", a.handlePut)
	mux.HandleFunc("GET /objects/{bucket}/{objectID}", a.handleGet)
	mux.HandleFunc("DELETE /objects/{bucket}/{objectID}", a.handleDelete)

	var h http.Handler = mux
	h = WithMaxBodyBytes(a.maxBodyBytes, h)
	h = WithLogging(a.logger, h)
	return h
}
