package service

import (
	"context"
	"github.com/google/uuid"
)

type AcceptAnyCategory struct{}

func (a *AcceptAnyCategory) Exists(_ context.Context, _ uuid.UUID) (bool, error) { return true, nil }
func NewAcceptAnyCategory() CategoryVerifier                                     { return &AcceptAnyCategory{} }

type AcceptAnyBrand struct{}

func (a *AcceptAnyBrand) Exists(_ context.Context, _ uuid.UUID) (bool, error) { return true, nil }
func NewAcceptAnyBrand() BrandVerifier                                        { return &AcceptAnyBrand{} }

type AcceptAnyUOM struct{}

func (a *AcceptAnyUOM) Exists(_ context.Context, _ uuid.UUID) (bool, error) { return true, nil }
func NewAcceptAnyUOM() UOMVerifier                                          { return &AcceptAnyUOM{} }

type NoInventory struct{}

func (n *NoInventory) HasStock(_ uuid.UUID) (bool, error) { return false, nil }
func NewNoInventory() InventoryVerifier                   { return &NoInventory{} }

type NoStock struct{}

func (n *NoStock) HasMovements(_ uuid.UUID) (bool, error) { return false, nil }
func NewNoStock() StockVerifier                           { return &NoStock{} }
