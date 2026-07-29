package service

import (
	"context"
	"github.com/google/uuid"
)

type CategoryVerifier interface {
	Exists(ctx context.Context, id uuid.UUID) (bool, error)
}

type BrandVerifier interface {
	Exists(ctx context.Context, id uuid.UUID) (bool, error)
}

type UOMVerifier interface {
	Exists(ctx context.Context, id uuid.UUID) (bool, error)
}

type InventoryVerifier interface {
	HasStock(productID uuid.UUID) (bool, error)
}

type StockVerifier interface {
	HasMovements(productID uuid.UUID) (bool, error)
}
