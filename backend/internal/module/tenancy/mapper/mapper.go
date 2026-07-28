// Package mapper converts between entities and DTOs.
//
// LAYER RULE: conversion lives here and nowhere else. Handlers and services do
// not build DTOs inline. Centralising it means that when a field must be hidden
// from API output, there is exactly one place to change — and one place to
// review when asking "can this field leak?".
package mapper

import (
	"github.com/batokhehe/wms-saas/backend/internal/module/tenancy/dto"
	"github.com/batokhehe/wms-saas/backend/internal/module/tenancy/entity"
	"github.com/batokhehe/wms-saas/backend/internal/shared/pagination"
)

// ---------- Company ----------

// ToCompanyResponse converts a company into its API representation.
func ToCompanyResponse(c *entity.Company) dto.CompanyResponse {
	if c == nil {
		return dto.CompanyResponse{}
	}

	return dto.CompanyResponse{
		ID:        c.ID,
		Code:      c.Code,
		Name:      c.Name,
		Email:     c.Email,
		Phone:     c.Phone,
		Logo:      c.Logo,
		Address:   c.Address,
		Status:    string(c.Status),
		CreatedAt: c.CreatedAt,
		UpdatedAt: c.UpdatedAt,
	}
}

// ToCompanyPage converts a page of companies into a page of DTOs, preserving
// the pagination metadata.
func ToCompanyPage(page pagination.Page[entity.Company]) pagination.Page[dto.CompanyResponse] {
	return pagination.MapPage(page, func(c entity.Company) dto.CompanyResponse {
		return ToCompanyResponse(&c)
	})
}

// FromCreateCompanyRequest builds a new company from a create request.
//
// The code is normalised on the way in, so the stored form is canonical. No ID
// is assigned here: identifier generation belongs to the repository, which
// holds the injected port.IDGenerator.
//
// Status is hard-coded rather than taken from the request — a client must not
// be able to create a company that is already SUSPENDED.
func FromCreateCompanyRequest(req dto.CreateCompanyRequest) entity.Company {
	return entity.Company{
		Code:    entity.NormalizeCode(req.Code),
		Name:    req.Name,
		Email:   req.Email,
		Phone:   req.Phone,
		Logo:    req.Logo,
		Address: req.Address,
		Status:  entity.CompanyActive,
	}
}

// ApplyUpdateCompanyRequest applies a partial update, leaving omitted fields
// untouched.
//
// Code is absent by design: it appears on printed documents and in external
// integrations, so renaming it silently invalidates references the system does
// not control.
func ApplyUpdateCompanyRequest(c *entity.Company, req dto.UpdateCompanyRequest) {
	if req.Name != nil {
		c.Name = *req.Name
	}
	if req.Email != nil {
		c.Email = *req.Email
	}
	if req.Phone != nil {
		c.Phone = *req.Phone
	}
	if req.Logo != nil {
		c.Logo = *req.Logo
	}
	if req.Address != nil {
		c.Address = *req.Address
	}
	if req.Status != nil {
		c.Status = entity.CompanyStatus(*req.Status)
	}
}

// ---------- Membership ----------

// ToMembershipResponse converts a membership into its API representation.
func ToMembershipResponse(m *entity.Membership) dto.MembershipResponse {
	if m == nil {
		return dto.MembershipResponse{}
	}

	return dto.MembershipResponse{
		ID:        m.ID,
		CompanyID: m.CompanyID,
		UserID:    m.UserID,
		Role:      string(m.Role),
		Status:    string(m.Status),
		JoinedAt:  m.JoinedAt,
		InvitedBy: m.InvitedBy,
		CreatedAt: m.CreatedAt,
		UpdatedAt: m.UpdatedAt,
	}
}

// ToMembershipPage converts a page of memberships.
func ToMembershipPage(
	page pagination.Page[entity.Membership],
) pagination.Page[dto.MembershipResponse] {
	return pagination.MapPage(page, func(m entity.Membership) dto.MembershipResponse {
		return ToMembershipResponse(&m)
	})
}

// ToMembershipWithCompany pairs a membership with its company, for the company
// switcher view.
func ToMembershipWithCompany(
	m *entity.Membership,
	c *entity.Company,
) dto.MembershipWithCompanyResponse {
	return dto.MembershipWithCompanyResponse{
		MembershipID: m.ID,
		Role:         string(m.Role),
		Status:       string(m.Status),
		JoinedAt:     m.JoinedAt,
		Company:      ToCompanyResponse(c),
	}
}

// ToCurrentCompanyResponse renders the active company together with the
// caller's standing in it.
func ToCurrentCompanyResponse(
	c *entity.Company,
	m *entity.Membership,
) dto.CurrentCompanyResponse {
	return dto.CurrentCompanyResponse{
		Company:      ToCompanyResponse(c),
		MembershipID: m.ID,
		Role:         string(m.Role),
	}
}
