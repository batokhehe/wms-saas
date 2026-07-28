// Package mapper converts the Customer aggregate into transport DTOs.
//
// LAYER RULE: conversion lives here and nowhere else; aggregate → DTO only.
package mapper

import (
	"github.com/batokhehe/wms-saas/backend/internal/module/customer/dto"
	"github.com/batokhehe/wms-saas/backend/internal/module/customer/entity"
	"github.com/batokhehe/wms-saas/backend/internal/shared/pagination"
)

// ToResponse converts an aggregate into its API representation, reading through
// getters.
func ToResponse(c *entity.Customer) dto.CustomerResponse {
	if c == nil {
		return dto.CustomerResponse{}
	}
	address := c.Address()
	return dto.CustomerResponse{
		ID:         c.ID(),
		Code:       c.Code().String(),
		Name:       c.Name(),
		Email:      c.Email().String(),
		Phone:      c.Phone().String(),
		TaxNumber:  c.TaxNumber().String(),
		Address:    address.Street(),
		City:       address.City(),
		Province:   address.Province(),
		Country:    address.Country(),
		PostalCode: address.PostalCode(),
		Status:     c.Status().String(),
		CreatedBy:  c.CreatedBy(),
		UpdatedBy:  c.UpdatedBy(),
		CreatedAt:  c.CreatedAt(),
		UpdatedAt:  c.UpdatedAt(),
	}
}

// ToPage converts a page of aggregates, preserving the pagination metadata.
func ToPage(page pagination.Page[*entity.Customer]) pagination.Page[dto.CustomerResponse] {
	return pagination.MapPage(page, ToResponse)
}
