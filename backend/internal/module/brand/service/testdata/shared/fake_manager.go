package testutils

import (
	"context"
)

type NoOpManager struct{}

func (m *NoOpManager) RunInTransaction(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}
