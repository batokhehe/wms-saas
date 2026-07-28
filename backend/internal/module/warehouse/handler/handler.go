// Package handler is the warehouse module's HTTP layer.
//
// LAYER RULE: bind input, call the service, shape the response, return.
//
// Note what NO method contains: a permission check, a status comparison, or any
// reference to a business rule. Authorisation is declared in route/route.go;
// business rules are in the aggregate. A handler that checked either would be a
// handler duplicating a decision made elsewhere.
package handler

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/internal/module/warehouse/dto"
	"github.com/batokhehe/wms-saas/backend/internal/module/warehouse/service"
	"github.com/batokhehe/wms-saas/backend/internal/shared/appcontext"
	"github.com/batokhehe/wms-saas/backend/internal/shared/response"
	"github.com/batokhehe/wms-saas/backend/internal/shared/validator"
)

// Handler exposes the warehouse use cases over HTTP.
type Handler struct {
	service *service.Service
}

// New wires the handler to its service.
func New(svc *service.Service) *Handler {
	return &Handler{service: svc}
}

// Create handles POST /warehouses.
func (h *Handler) Create(c *gin.Context) {
	var req dto.CreateWarehouseRequest
	if err := validator.BindJSON(c, &req); err != nil {
		response.Error(c, err)
		return
	}

	result, err := h.service.Create(appcontext.Context(c), req)
	if err != nil {
		response.Error(c, err)
		return
	}

	response.Created(c, "Warehouse created successfully", result)
}

// List handles GET /warehouses.
func (h *Handler) List(c *gin.Context) {
	var query dto.ListWarehousesQuery
	if err := validator.BindQuery(c, &query); err != nil {
		response.Error(c, err)
		return
	}

	page, err := h.service.List(appcontext.Context(c), query)
	if err != nil {
		response.Error(c, err)
		return
	}

	response.Page(c, "Warehouses retrieved successfully", page)
}

// Get handles GET /warehouses/:id.
func (h *Handler) Get(c *gin.Context) {
	id, ok := h.param(c)
	if !ok {
		return
	}

	result, err := h.service.Get(appcontext.Context(c), id)
	if err != nil {
		response.Error(c, err)
		return
	}

	response.OK(c, "Warehouse retrieved successfully", result)
}

// Update handles PUT /warehouses/:id.
func (h *Handler) Update(c *gin.Context) {
	id, ok := h.param(c)
	if !ok {
		return
	}

	var req dto.UpdateWarehouseRequest
	if err := validator.BindJSON(c, &req); err != nil {
		response.Error(c, err)
		return
	}

	result, err := h.service.Update(appcontext.Context(c), id, req)
	if err != nil {
		response.Error(c, err)
		return
	}

	response.OK(c, "Warehouse updated successfully", result)
}

// Activate handles PATCH /warehouses/:id/activate.
func (h *Handler) Activate(c *gin.Context) {
	id, ok := h.param(c)
	if !ok {
		return
	}

	result, err := h.service.Activate(appcontext.Context(c), id)
	if err != nil {
		response.Error(c, err)
		return
	}

	response.OK(c, "Warehouse activated successfully", result)
}

// Deactivate handles PATCH /warehouses/:id/deactivate.
func (h *Handler) Deactivate(c *gin.Context) {
	id, ok := h.param(c)
	if !ok {
		return
	}

	result, err := h.service.Deactivate(appcontext.Context(c), id)
	if err != nil {
		response.Error(c, err)
		return
	}

	response.OK(c, "Warehouse deactivated successfully", result)
}

// Suspend handles PATCH /warehouses/:id/suspend.
func (h *Handler) Suspend(c *gin.Context) {
	id, ok := h.param(c)
	if !ok {
		return
	}

	var req dto.SuspendWarehouseRequest
	if err := validator.BindJSON(c, &req); err != nil {
		response.Error(c, err)
		return
	}

	result, err := h.service.Suspend(appcontext.Context(c), id, req)
	if err != nil {
		response.Error(c, err)
		return
	}

	response.OK(c, "Warehouse suspended successfully", result)
}

// ChangeContact handles PATCH /warehouses/:id/contact.
func (h *Handler) ChangeContact(c *gin.Context) {
	id, ok := h.param(c)
	if !ok {
		return
	}

	var req dto.ChangeContactRequest
	if err := validator.BindJSON(c, &req); err != nil {
		response.Error(c, err)
		return
	}

	result, err := h.service.ChangeContact(appcontext.Context(c), id, req)
	if err != nil {
		response.Error(c, err)
		return
	}

	response.OK(c, "Warehouse contact updated successfully", result)
}

// Archive handles PATCH /warehouses/:id/archive.
func (h *Handler) Archive(c *gin.Context) {
	id, ok := h.param(c)
	if !ok {
		return
	}

	if err := h.service.Archive(appcontext.Context(c), id); err != nil {
		response.Error(c, err)
		return
	}

	response.OK(c, "Warehouse archived successfully", nil)
}

// Delete handles DELETE /warehouses/:id.
//
// It performs the SAME archive operation. A warehouse is never hard-deleted, so
// there are not two behaviours here — DELETE is the REST-conventional alias for
// the domain's Archive, offered because clients expect it. Having both call one
// service method means they can never diverge.
func (h *Handler) Delete(c *gin.Context) {
	id, ok := h.param(c)
	if !ok {
		return
	}

	if err := h.service.Archive(appcontext.Context(c), id); err != nil {
		response.Error(c, err)
		return
	}

	response.OK(c, "Warehouse archived successfully", nil)
}

// param binds and parses the :id path parameter.
//
// It writes the error response itself and reports whether the caller should
// continue, so the two-step bind-then-parse does not appear in eight handlers.
func (h *Handler) param(c *gin.Context) (id uuid.UUID, ok bool) {
	var param dto.IDParam
	if err := validator.BindURI(c, &param); err != nil {
		response.Error(c, err)
		return id, false
	}

	parsed, err := param.UUID()
	if err != nil {
		response.Error(c, err)
		return id, false
	}

	return parsed, true
}
