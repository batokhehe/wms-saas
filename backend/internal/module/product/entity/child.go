package entity

import (
	"github.com/google/uuid"
)

type ProductBarcode struct {
	ID, ProductID uuid.UUID
	Code          string
	Type          string
	IsPrimary     bool
}

type AlternateUOM struct {
	ID, ProductID, UOMID uuid.UUID
	Factor               float64
}
