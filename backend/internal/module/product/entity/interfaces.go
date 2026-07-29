package entity

import (
	"github.com/google/uuid"
)

type InventoryVerifier interface {
	HasStock(productID uuid.UUID) (bool, error)
}

type StockVerifier interface {
	HasMovements(productID uuid.UUID) (bool, error)
}
