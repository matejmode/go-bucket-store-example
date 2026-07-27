package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"

	"bucketstore/internal/store"
)

// handlePut implements PUT /objects/{bucket}/{objectID}: reads the request
// body, stores it (deduplicating by content hash within the bucket), and
// responds 201 Created with {"id": objectID} on success.
func (a *API) handlePut(w http.ResponseWriter, r *http.Request) {
	bucket, objectID := r.PathValue("bucket"), r.PathValue("objectID")
	if !validName(bucket) || !validName(objectID) {
		http.Error(w, "invalid bucket or object id", http.StatusBadRequest)
		return
	}

	// Pre-size the buffer from Content-Length when the client sends one, so
	// a large upload doesn't pay for io.ReadAll's repeated grow-and-copy
	// reallocations on its way to a known final size.
	var buf bytes.Buffer
	if r.ContentLength > 0 {
		buf.Grow(int(r.ContentLength))
	}
	if _, err := buf.ReadFrom(r.Body); err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "failed to read request body", http.StatusBadRequest)
		return
	}
	body := buf.Bytes()

	if err := a.store.Put(bucket, objectID, body); err != nil {
		a.writeStoreError(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]string{"id": objectID})
}

// handleGet implements GET /objects/{bucket}/{objectID}: responds 200 OK
// with the stored raw bytes, or 404 Not Found if the object doesn't exist.
func (a *API) handleGet(w http.ResponseWriter, r *http.Request) {
	bucket, objectID := r.PathValue("bucket"), r.PathValue("objectID")
	if !validName(bucket) || !validName(objectID) {
		http.Error(w, "invalid bucket or object id", http.StatusBadRequest)
		return
	}

	data, err := a.store.Get(bucket, objectID)
	if err != nil {
		a.writeStoreError(w, err)
		return
	}

	// Objects are stored and served as opaque bytes - there's no
	// client-supplied Content-Type to trust or reflect, so every response
	// is generic binary. nosniff stops a browser from guessing otherwise.
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Type", "application/octet-stream")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// handleDelete implements DELETE /objects/{bucket}/{objectID}: responds
// 200 OK if the object existed and was removed, or 404 Not Found otherwise.
func (a *API) handleDelete(w http.ResponseWriter, r *http.Request) {
	bucket, objectID := r.PathValue("bucket"), r.PathValue("objectID")
	if !validName(bucket) || !validName(objectID) {
		http.Error(w, "invalid bucket or object id", http.StatusBadRequest)
		return
	}

	if err := a.store.Delete(bucket, objectID); err != nil {
		a.writeStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// writeStoreError maps an error returned by the store package to the
// appropriate HTTP status: 404 for ErrNotFound, 400 for ErrInvalidName, and
// 500 (logging the underlying error server-side) for anything else.
func (a *API) writeStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		http.Error(w, "not found", http.StatusNotFound)
	case errors.Is(err, store.ErrInvalidName):
		http.Error(w, "invalid bucket or object id", http.StatusBadRequest)
	default:
		a.logger.Error("store operation failed", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}
}
