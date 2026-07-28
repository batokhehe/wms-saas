// Package mapper converts between entities and DTOs.
//
// LAYER RULE: conversion lives here and nowhere else. Handlers and services do
// not build DTOs inline.
package mapper

import (
	"sort"

	"github.com/batokhehe/wms-saas/backend/internal/module/rbac/dto"
	"github.com/batokhehe/wms-saas/backend/internal/module/rbac/entity"
	"github.com/batokhehe/wms-saas/backend/internal/shared/pagination"
)

// ToPermissionResponse converts a catalogue entry.
func ToPermissionResponse(p *entity.Permission) dto.PermissionResponse {
	if p == nil {
		return dto.PermissionResponse{}
	}

	return dto.PermissionResponse{
		ID:     p.ID,
		Code:   p.Code.String(),
		Name:   p.Name,
		Module: p.Module,
	}
}

// ToPermissionResponses converts a slice of catalogue entries.
//
// Returns an empty slice rather than nil so the JSON encoder emits [] and not
// null. A client that has to handle both will eventually crash on one.
func ToPermissionResponses(permissions []entity.Permission) []dto.PermissionResponse {
	result := make([]dto.PermissionResponse, 0, len(permissions))
	for i := range permissions {
		result = append(result, ToPermissionResponse(&permissions[i]))
	}
	return result
}

// ToRoleResponse converts a role together with its resolved permission codes.
//
// The permissions are passed in rather than read from the entity because Role
// has no permissions field — the grants live in a separate table, and giving
// the entity a slice would invite a lazily-populated field that is sometimes
// loaded and sometimes not.
func ToRoleResponse(r *entity.Role, permissions []entity.Code) dto.RoleResponse {
	if r == nil {
		return dto.RoleResponse{}
	}

	return dto.RoleResponse{
		ID:          r.ID,
		Name:        r.Name,
		Description: r.Description,
		IsSystem:    r.IsSystem,
		Permissions: SortedCodes(permissions),
		CreatedAt:   r.CreatedAt,
		UpdatedAt:   r.UpdatedAt,
	}
}

// ToRolePage converts a page of roles, looking each role's permissions up in
// the supplied map.
//
// The map is built by ONE query over every role in the page rather than one
// query per role — the N+1 problem this endpoint would otherwise have, and the
// most common cause of a list endpoint that is fast in development and unusable
// in production.
func ToRolePage(
	page pagination.Page[entity.Role],
	permissionsByRole map[entity.RoleID][]entity.Code,
) pagination.Page[dto.RoleResponse] {
	return pagination.MapPage(page, func(r entity.Role) dto.RoleResponse {
		return ToRoleResponse(&r, permissionsByRole[r.ID])
	})
}

// FromCreateRoleRequest builds a new role from a create request.
//
// The name is normalised on the way in, so the stored form is canonical and
// comparable with memberships.role. IsSystem is hard-coded false: system status
// is conferred by the provisioner, never by a caller.
func FromCreateRoleRequest(req dto.CreateRoleRequest, companyID entity.RoleID) entity.Role {
	return entity.Role{
		CompanyID:   companyID,
		Name:        entity.NormalizeRoleName(req.Name),
		Description: req.Description,
		IsSystem:    false,
	}
}

// ApplyUpdateRoleRequest applies a partial update, leaving omitted fields
// untouched.
//
// Name is absent by design — see dto.UpdateRoleRequest.
func ApplyUpdateRoleRequest(r *entity.Role, req dto.UpdateRoleRequest) {
	if req.Description != nil {
		r.Description = *req.Description
	}
}

// ToMyPermissionsResponse reports the caller's effective permissions.
func ToMyPermissionsResponse(roleName string, set entity.Set) dto.MyPermissionsResponse {
	return dto.MyPermissionsResponse{
		Role:        roleName,
		Permissions: SortedCodes(set.Codes()),
	}
}

// SortedCodes renders permission codes as a stable, sorted string slice.
//
// Sorted because entity.Set is a map and Go randomises map iteration: without
// this, two identical roles would serialise differently on consecutive
// requests, breaking client-side caching and making API responses impossible to
// diff in a test.
func SortedCodes(codes []entity.Code) []string {
	result := make([]string, 0, len(codes))
	for _, code := range codes {
		result = append(result, code.String())
	}
	sort.Strings(result)
	return result
}
