// Package handler is the RBAC module's HTTP layer.
//
// LAYER RULE: bind input, call the service, shape the response, return.
//
// Note what NO method below contains: a permission check. Authorisation is
// declared in route/route.go and enforced by middleware, so the route table is
// the single place a reviewer reads to answer "who can do this?". A check
// written here would be a check that table does not show.
package handler

import (
	"github.com/gin-gonic/gin"

	"github.com/batokhehe/wms-saas/backend/internal/module/rbac/dto"
	"github.com/batokhehe/wms-saas/backend/internal/module/rbac/service"
	"github.com/batokhehe/wms-saas/backend/internal/shared/appcontext"
	"github.com/batokhehe/wms-saas/backend/internal/shared/response"
	"github.com/batokhehe/wms-saas/backend/internal/shared/validator"
)

// RoleHandler exposes the role use cases over HTTP.
type RoleHandler struct {
	service *service.RoleService
}

// NewRoleHandler wires the handler to its service.
func NewRoleHandler(svc *service.RoleService) *RoleHandler {
	return &RoleHandler{service: svc}
}

// List handles GET /roles.
func (h *RoleHandler) List(c *gin.Context) {
	var query dto.ListRolesQuery
	if err := validator.BindQuery(c, &query); err != nil {
		response.Error(c, err)
		return
	}

	page, err := h.service.List(appcontext.Context(c), query)
	if err != nil {
		response.Error(c, err)
		return
	}

	response.Page(c, "Roles retrieved successfully", page)
}

// Create handles POST /roles.
func (h *RoleHandler) Create(c *gin.Context) {
	var req dto.CreateRoleRequest
	if err := validator.BindJSON(c, &req); err != nil {
		response.Error(c, err)
		return
	}

	result, err := h.service.Create(appcontext.Context(c), req)
	if err != nil {
		response.Error(c, err)
		return
	}

	response.Created(c, "Role created successfully", result)
}

// Update handles PUT /roles/:id.
func (h *RoleHandler) Update(c *gin.Context) {
	var param dto.IDParam
	if err := validator.BindURI(c, &param); err != nil {
		response.Error(c, err)
		return
	}

	var req dto.UpdateRoleRequest
	if err := validator.BindJSON(c, &req); err != nil {
		response.Error(c, err)
		return
	}

	id, err := param.UUID()
	if err != nil {
		response.Error(c, err)
		return
	}

	result, err := h.service.Update(appcontext.Context(c), id, req)
	if err != nil {
		response.Error(c, err)
		return
	}

	response.OK(c, "Role updated successfully", result)
}

// Delete handles DELETE /roles/:id.
func (h *RoleHandler) Delete(c *gin.Context) {
	var param dto.IDParam
	if err := validator.BindURI(c, &param); err != nil {
		response.Error(c, err)
		return
	}

	id, err := param.UUID()
	if err != nil {
		response.Error(c, err)
		return
	}

	if err := h.service.Delete(appcontext.Context(c), id); err != nil {
		response.Error(c, err)
		return
	}

	response.OK(c, "Role deleted successfully", nil)
}

// SetPermissions handles PUT /roles/:id/permissions.
func (h *RoleHandler) SetPermissions(c *gin.Context) {
	var param dto.IDParam
	if err := validator.BindURI(c, &param); err != nil {
		response.Error(c, err)
		return
	}

	var req dto.SetRolePermissionsRequest
	if err := validator.BindJSON(c, &req); err != nil {
		response.Error(c, err)
		return
	}

	id, err := param.UUID()
	if err != nil {
		response.Error(c, err)
		return
	}

	result, err := h.service.SetPermissions(appcontext.Context(c), id, req)
	if err != nil {
		response.Error(c, err)
		return
	}

	response.OK(c, "Role permissions updated successfully", result)
}

// PermissionHandler exposes the permission catalogue over HTTP.
type PermissionHandler struct {
	service *service.PermissionService
}

// NewPermissionHandler wires the handler to its service.
func NewPermissionHandler(svc *service.PermissionService) *PermissionHandler {
	return &PermissionHandler{service: svc}
}

// List handles GET /permissions.
func (h *PermissionHandler) List(c *gin.Context) {
	var query dto.ListPermissionsQuery
	if err := validator.BindQuery(c, &query); err != nil {
		response.Error(c, err)
		return
	}

	result, err := h.service.List(appcontext.Context(c), query)
	if err != nil {
		response.Error(c, err)
		return
	}

	response.OK(c, "Permissions retrieved successfully", result)
}

// Mine handles GET /permissions/mine.
//
// Deliberately unguarded by any permission: a caller must always be able to
// discover what they themselves may do, and requiring permission.read to find
// out whether you have permission.read is circular.
func (h *PermissionHandler) Mine(c *gin.Context) {
	result, err := h.service.Mine(appcontext.Context(c))
	if err != nil {
		response.Error(c, err)
		return
	}

	response.OK(c, "Permissions retrieved successfully", result)
}
