package api

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"bucketstore/internal/store"
)

func newTestAPI() http.Handler {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := store.NewObjectStore(store.NewMemoryBlobStore(), nil)
	return New(s, logger, 1<<20).Routes()
}

// flakyBlobStore wraps a real BlobStore and lets a test force any of its
// methods to fail, to exercise the API layer's handling of an internal
// (non-ErrNotFound/ErrInvalidName) storage error - i.e. the 500 path.
type flakyBlobStore struct {
	next                         store.BlobStore
	failPut, failGet, failDelete bool
}

var errFlaky = errors.New("simulated storage failure")

func (f *flakyBlobStore) Put(bucket, hash string, data []byte) error {
	if f.failPut {
		return errFlaky
	}
	return f.next.Put(bucket, hash, data)
}

func (f *flakyBlobStore) Get(bucket, hash string) ([]byte, error) {
	if f.failGet {
		return nil, errFlaky
	}
	return f.next.Get(bucket, hash)
}

func (f *flakyBlobStore) Delete(bucket, hash string) error {
	if f.failDelete {
		return errFlaky
	}
	return f.next.Delete(bucket, hash)
}

func TestPutGetDeleteLifecycle(t *testing.T) {
	h := newTestAPI()

	// PUT creates the object.
	putReq := httptest.NewRequest(http.MethodPut, "/objects/b1/obj1", strReader("hello world"))
	putW := httptest.NewRecorder()
	h.ServeHTTP(putW, putReq)

	if putW.Code != http.StatusCreated {
		t.Fatalf("PUT status = %d, want %d; body = %s", putW.Code, http.StatusCreated, putW.Body.String())
	}
	if ct := putW.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("PUT response Content-Type = %q, want application/json", ct)
	}
	wantBody := `{"id":"obj1"}` + "\n"
	if putW.Body.String() != wantBody {
		t.Errorf("PUT body = %q, want %q", putW.Body.String(), wantBody)
	}

	// GET returns the raw bytes back as generic binary.
	getReq := httptest.NewRequest(http.MethodGet, "/objects/b1/obj1", nil)
	getW := httptest.NewRecorder()
	h.ServeHTTP(getW, getReq)

	if getW.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want %d", getW.Code, http.StatusOK)
	}
	if getW.Body.String() != "hello world" {
		t.Errorf("GET body = %q, want %q", getW.Body.String(), "hello world")
	}
	if ct := getW.Header().Get("Content-Type"); ct != "application/octet-stream" {
		t.Errorf("GET Content-Type = %q, want application/octet-stream", ct)
	}

	// DELETE removes it.
	delReq := httptest.NewRequest(http.MethodDelete, "/objects/b1/obj1", nil)
	delW := httptest.NewRecorder()
	h.ServeHTTP(delW, delReq)
	if delW.Code != http.StatusOK {
		t.Fatalf("DELETE status = %d, want %d", delW.Code, http.StatusOK)
	}

	// Now it's gone.
	getReq2 := httptest.NewRequest(http.MethodGet, "/objects/b1/obj1", nil)
	getW2 := httptest.NewRecorder()
	h.ServeHTTP(getW2, getReq2)
	if getW2.Code != http.StatusNotFound {
		t.Errorf("GET after DELETE status = %d, want %d", getW2.Code, http.StatusNotFound)
	}

	// Deleting again is also 404.
	delReq2 := httptest.NewRequest(http.MethodDelete, "/objects/b1/obj1", nil)
	delW2 := httptest.NewRecorder()
	h.ServeHTTP(delW2, delReq2)
	if delW2.Code != http.StatusNotFound {
		t.Errorf("second DELETE status = %d, want %d", delW2.Code, http.StatusNotFound)
	}
}

func TestGetResponseSetsNoSniff(t *testing.T) {
	h := newTestAPI()
	putReq := httptest.NewRequest(http.MethodPut, "/objects/b1/obj1", strReader("<script>alert(1)</script>"))
	h.ServeHTTP(httptest.NewRecorder(), putReq)

	getReq := httptest.NewRequest(http.MethodGet, "/objects/b1/obj1", nil)
	getW := httptest.NewRecorder()
	h.ServeHTTP(getW, getReq)

	if got := getW.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want %q", got, "nosniff")
	}
}

func TestGetMissingObjectReturns404(t *testing.T) {
	h := newTestAPI()
	req := httptest.NewRequest(http.MethodGet, "/objects/b1/nope", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestInvalidNameReturns400(t *testing.T) {
	h := newTestAPI()
	req := httptest.NewRequest(http.MethodPut, "/objects/bad%2Fname/obj1", strReader("x"))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestInvalidNameReturns400ForGetAndDelete(t *testing.T) {
	h := newTestAPI()

	getReq := httptest.NewRequest(http.MethodGet, "/objects/bad%2Fname/obj1", nil)
	getW := httptest.NewRecorder()
	h.ServeHTTP(getW, getReq)
	if getW.Code != http.StatusBadRequest {
		t.Errorf("GET status = %d, want %d", getW.Code, http.StatusBadRequest)
	}

	delReq := httptest.NewRequest(http.MethodDelete, "/objects/bad%2Fname/obj1", nil)
	delW := httptest.NewRecorder()
	h.ServeHTTP(delW, delReq)
	if delW.Code != http.StatusBadRequest {
		t.Errorf("DELETE status = %d, want %d", delW.Code, http.StatusBadRequest)
	}
}

func TestBodyOverLimitReturns413(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := store.NewObjectStore(store.NewMemoryBlobStore(), nil)
	h := New(s, logger, 4).Routes() // tiny 4-byte limit

	req := httptest.NewRequest(http.MethodPut, "/objects/b1/obj1", strReader("way too much data"))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want %d", w.Code, http.StatusRequestEntityTooLarge)
	}
}

func TestDedupAcrossObjectIDsInSameBucket(t *testing.T) {
	h := newTestAPI()

	put := func(objectID, body string) int {
		req := httptest.NewRequest(http.MethodPut, "/objects/b1/"+objectID, strReader(body))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		return w.Code
	}

	if code := put("obj1", "same content"); code != http.StatusCreated {
		t.Fatalf("PUT obj1 status = %d", code)
	}
	if code := put("obj2", "same content"); code != http.StatusCreated {
		t.Fatalf("PUT obj2 status = %d", code)
	}

	// Deleting obj1 must not affect obj2's ability to be read back.
	delReq := httptest.NewRequest(http.MethodDelete, "/objects/b1/obj1", nil)
	delW := httptest.NewRecorder()
	h.ServeHTTP(delW, delReq)
	if delW.Code != http.StatusOK {
		t.Fatalf("DELETE obj1 status = %d", delW.Code)
	}

	getReq := httptest.NewRequest(http.MethodGet, "/objects/b1/obj2", nil)
	getW := httptest.NewRecorder()
	h.ServeHTTP(getW, getReq)
	if getW.Code != http.StatusOK || getW.Body.String() != "same content" {
		t.Errorf("GET obj2 = (%d, %q), want (200, %q)", getW.Code, getW.Body.String(), "same content")
	}
}

func TestInternalErrorOnPutReturns500(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	fb := &flakyBlobStore{next: store.NewMemoryBlobStore(), failPut: true}
	s := store.NewObjectStore(fb, nil)
	h := New(s, logger, 1<<20).Routes()

	req := httptest.NewRequest(http.MethodPut, "/objects/b1/obj1", strReader("x"))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
}

func TestInternalErrorOnGetReturns500(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	fb := &flakyBlobStore{next: store.NewMemoryBlobStore()}
	s := store.NewObjectStore(fb, nil)
	h := New(s, logger, 1<<20).Routes()

	putReq := httptest.NewRequest(http.MethodPut, "/objects/b1/obj1", strReader("x"))
	h.ServeHTTP(httptest.NewRecorder(), putReq)

	fb.failGet = true
	getReq := httptest.NewRequest(http.MethodGet, "/objects/b1/obj1", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, getReq)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
}

func TestInternalErrorOnDeleteReturns500(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	fb := &flakyBlobStore{next: store.NewMemoryBlobStore()}
	s := store.NewObjectStore(fb, nil)
	h := New(s, logger, 1<<20).Routes()

	putReq := httptest.NewRequest(http.MethodPut, "/objects/b1/obj1", strReader("x"))
	h.ServeHTTP(httptest.NewRecorder(), putReq)

	fb.failDelete = true
	delReq := httptest.NewRequest(http.MethodDelete, "/objects/b1/obj1", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, delReq)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
}

func TestWriteStoreErrorMapsErrInvalidNameTo400(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	a := New(store.NewObjectStore(store.NewMemoryBlobStore(), nil), logger, 1<<20)

	w := httptest.NewRecorder()
	a.writeStoreError(w, store.ErrInvalidName)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestInvalidNameEdgeCases(t *testing.T) {
	h := newTestAPI()

	longName := strings.Repeat("a", 256) // one over the 255 limit
	cases := []struct {
		name   string
		bucket string
	}{
		{"too long", longName},
		{"disallowed char", "b@d"},
		{"encoded space", "has%20space"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPut, "/objects/"+tc.bucket+"/obj1", strReader("x"))
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			if w.Code != http.StatusBadRequest {
				t.Errorf("bucket=%q: status = %d, want %d", tc.bucket, w.Code, http.StatusBadRequest)
			}
		})
	}
}

// TestDotSegmentsNeverReachTheHandler documents a discovery from the second
// security review pass: requests with a literal "." or ".." bucket segment
// never reach our validName check at all. net/http's ServeMux cleans the
// path and issues a 307 redirect first (preserving method and body, unlike
// 301/302), so validName's explicit "." / ".." rejection is unreachable
// dead code on this exact HTTP surface - it only matters as defense-in-depth
// for a caller that uses the store package directly, without ServeMux in
// front of it.
func TestDotSegmentsNeverReachTheHandler(t *testing.T) {
	h := newTestAPI()

	for _, bucket := range []string{".", ".."} {
		req := httptest.NewRequest(http.MethodPut, "/objects/"+bucket+"/obj1", strReader("x"))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)

		if w.Code != http.StatusTemporaryRedirect {
			t.Errorf("bucket=%q: status = %d, want %d (ServeMux path cleaning)", bucket, w.Code, http.StatusTemporaryRedirect)
		}
		// Whatever it redirects to must not be our route with a live
		// object underneath - it should 404 rather than silently write to
		// or read from something.
		location := w.Header().Get("Location")
		followReq := httptest.NewRequest(http.MethodPut, location, strReader("x"))
		followW := httptest.NewRecorder()
		h.ServeHTTP(followW, followReq)
		if followW.Code == http.StatusCreated {
			t.Errorf("bucket=%q redirected to %q, which the API accepted as a valid PUT - traversal not fully blocked", bucket, location)
		}
	}
}

func TestValidNameDirect(t *testing.T) {
	// validName's explicit "." / ".." rejection is unreachable through the
	// live HTTP mux (see TestDotSegmentsNeverReachTheHandler) - net/http's
	// path cleaning redirects those away first. Tested directly here so the
	// function itself is still verified in isolation, independent of that
	// routing behavior.
	cases := []struct {
		name string
		want bool
	}{
		{"bucket1", true},
		{".", false},
		{"..", false},
		{"", false},
		{"a/b", false},
	}
	for _, tc := range cases {
		if got := validName(tc.name); got != tc.want {
			t.Errorf("validName(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func strReader(s string) io.Reader {
	return strings.NewReader(s)
}
