// Package storage will hold the object-store adapters (MinIO, S3).
//
// No backend is implemented yet. What ships here is a null object that
// satisfies port.Storage by failing loudly, so that bootstrap can wire a
// non-nil Storage today and modules can be written and compiled against the
// real interface before a bucket exists.
//
// The alternative — leaving Dependencies.Storage nil until MinIO lands — trades
// a clear, logged 503 for a nil-pointer panic in production.
package storage

import (
	"context"
	"io"
	"time"

	"github.com/batokhehe/wms-saas/backend/internal/shared/port"
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// Unavailable is a port.Storage that rejects every call.
//
// It returns a 503 rather than a 500: an unconfigured backend is an
// availability problem an operator can fix, not a bug in the calling code.
type Unavailable struct{}

var _ port.Storage = (*Unavailable)(nil)

// NewUnavailable builds the null storage backend.
func NewUnavailable() *Unavailable { return &Unavailable{} }

func notConfigured(op string) error {
	return apperror.Unavailable("File storage is not configured").
		WithOp("storage." + op)
}

func (Unavailable) Put(context.Context, string, io.Reader, port.PutOptions) (port.ObjectInfo, error) {
	return port.ObjectInfo{}, notConfigured("Put")
}

func (Unavailable) Get(context.Context, string) (io.ReadCloser, error) {
	return nil, notConfigured("Get")
}

func (Unavailable) Stat(context.Context, string) (port.ObjectInfo, error) {
	return port.ObjectInfo{}, notConfigured("Stat")
}

func (Unavailable) Exists(context.Context, string) (bool, error) {
	return false, notConfigured("Exists")
}

func (Unavailable) Delete(context.Context, string) error {
	return notConfigured("Delete")
}

func (Unavailable) List(context.Context, string, int) ([]port.ObjectInfo, error) {
	return nil, notConfigured("List")
}

func (Unavailable) SignedURL(context.Context, string, port.SignedURLMethod, time.Duration) (string, error) {
	return "", notConfigured("SignedURL")
}
