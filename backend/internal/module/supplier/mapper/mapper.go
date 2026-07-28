// Package mapper converts the Supplier aggregate into transport DTOs.
//
// LAYER RULE: conversion lives here and nowhere else. The direction is aggregate
// → DTO only; building an aggregate is the factory's job.
package mapper

import (
	"github.com/batokhehe/wms-saas/backend/internal/module/supplier/dto"
	"github.com/batokhehe/wms-saas/backend/internal/module/supplier/entity"
	"github.com/batokhehe/wms-saas/backend/internal/shared/pagination"
)

// ToResponse converts an aggregate into its API representation, reading through
// getters — the mapper is subject to the same encapsulation as every caller.
func ToResponse(s *entity.Supplier) dto.SupplierResponse {
	if s == nil {
		return dto.SupplierResponse{}
	}
	address := s.Address()
	return dto.SupplierResponse{
		ID:         s.ID(),
		Code:       s.Code().String(),
		Name:       s.Name(),
		Email:      s.Email().String(),
		Phone:      s.Phone().String(),
		TaxNumber:  s.TaxNumber().String(),
		Address:    address.Street(),
		City:       address.City(),
		Province:   address.Province(),
		Country:    address.Country(),
		PostalCode: address.PostalCode(),
		Status:     s.Status().String(),
		CreatedBy:  s.CreatedBy(),
		UpdatedBy:  s.UpdatedBy(),
		CreatedAt:  s.CreatedAt(),
		UpdatedAt:  s.UpdatedAt(),
	}
}

// ToPage converts a page of aggregates, preserving the pagination metadata.
func ToPage(page pagination.Page[*entity.Supplier]) pagination.Page[dto.SupplierResponse] {
	return pagination.MapPage(page, ToResponse)
}
