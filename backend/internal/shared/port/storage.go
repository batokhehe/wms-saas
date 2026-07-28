package port

import (
	"context"
	"errors"
	"io"
	"time"
)

// ErrObjectNotFound is returned when the requested key does not exist.
var ErrObjectNotFound = errors.New("storage: object not found")

// ObjectInfo is the metadata of a stored object.
type ObjectInfo struct {
	Key         string
	Size        int64
	ContentType string
	ETag        string
	ModifiedAt  time.Time
}

// PutOptions describes how an object should be written.
type PutOptions struct {
	ContentType string
	// Size is the payload length. S3-compatible backends stream far more
	// efficiently when the length is known up front; -1 means unknown.
	Size int64
	// Metadata is stored alongside the object as user-defined attributes.
	Metadata map[string]string
	// Public marks the object world-readable where the backend supports it.
	Public bool
}

// SignedURLMethod is the operation a pre-signed URL authorises.
type SignedURLMethod string

const (
	SignedURLGet SignedURLMethod = "GET"
	SignedURLPut SignedURLMethod = "PUT"
)

// Storage is an object store for files: shipping labels, product images,
// exported reports, import spreadsheets.
//
// It is defined as an interface with no implementation yet, deliberately. Doing
// the abstraction first means the first module that needs file storage writes
// against a stable contract instead of hard-coding MinIO calls that later have
// to be untangled.
//
// The interface is intentionally S3-shaped (keys, prefixes, pre-signed URLs)
// because both planned backends are S3-compatible. It is not, however,
// S3-typed: no aws-sdk or minio-go type appears in any signature, so a purely
// local filesystem implementation for tests remains straightforward.
type Storage interface {
	// Put writes an object, overwriting any existing object at key.
	Put(ctx context.Context, key string, r io.Reader, opts PutOptions) (ObjectInfo, error)

	// Get opens an object for reading. The caller must close the reader.
	// Returns ErrObjectNotFound when key is absent.
	Get(ctx context.Context, key string) (io.ReadCloser, error)

	// Stat returns metadata without transferring the object body.
	Stat(ctx context.Context, key string) (ObjectInfo, error)

	// Exists reports whether key is present.
	Exists(ctx context.Context, key string) (bool, error)

	// Delete removes an object. Deleting an absent key is not an error.
	Delete(ctx context.Context, key string) error

	// List returns objects under prefix, capped at limit.
	List(ctx context.Context, prefix string, limit int) ([]ObjectInfo, error)

	// SignedURL issues a time-limited URL for a direct client transfer.
	//
	// This is the reason the abstraction matters for a WMS: letting the Flutter
	// client upload a 20 MB delivery photo straight to the bucket keeps that
	// payload out of the API process entirely.
	SignedURL(ctx context.Context, key string, method SignedURLMethod, ttl time.Duration) (string, error)
}
