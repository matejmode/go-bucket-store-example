package store

import "errors"

// ErrNotFound is returned when a requested object, bucket, or blob does not exist.
var ErrNotFound = errors.New("not found")

// ErrInvalidName is returned when a bucket or object identifier fails validation.
var ErrInvalidName = errors.New("invalid name")
