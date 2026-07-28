// Package handler is the supplier module's HTTP layer.
//
// LAYER RULE: bind input, call the service, shape the response, return. No method
// contains a permission check, a status comparison or a business rule.
package handler

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/internal/module/supplier/dto"
	"github.com/batokhehe/wms-saas/backend/internal/module/supplier/service"
	"github.com/batokhehe/wms-saas/backend/internal/shared/appcontext"
	"github.com/batokhehe/wms-saas/backend/internal/shared/response"
	"github.com/batokhehe/wms-saas/backend/internal/shared/validator"
)

// Handler exposes the supplier use cases over HTTP.
type Handler struct {
	service *service.Service
}

// New wires the handler to its service.
func New(svc *service.Service) *Handler {
	return &Handler{service: svc}
}

// Create handles POST /suppliers.
func (h *Handler) Create(c *gin.Context) {
	var req dto.CreateSupplierRequest
	if err := validator.BindJSON(c, &req); err != nil {
		response.Error(c, err)
		return
	}
	result, err := h.service.Create(appcontext.Context(c), req)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Created(c, "Supplier created successfully", result)
}

// List handles GET /suppliers.
func (h *Handler) List(c *gin.Context) {
	var query dto.ListSuppliersQuery
	if err := validator.BindQuery(c, &query); err != nil {
		response.Error(c, err)
		return
	}
	page, err := h.service.List(appcontext.Context(c), query)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Page(c, "Suppliers retrieved successfully", page)
}

// Get handles GET /suppliers/:id.
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
	response.OK(c, "Supplier retrieved successfully", result)
}

// Update handles PUT /suppliers/:id.
func (h *Handler) Update(c *gin.Context) {
	id, ok := h.param(c)
	if !ok {
		return
	}
	var req dto.UpdateSupplierRequest
	if err := validator.BindJSON(c, &req); err != nil {
		response.Error(c, err)
		return
	}
	result, err := h.service.Update(appcontext.Context(c), id, req)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, "Supplier updated successfully", result)
}

// Activate handles PATCH /suppliers/:id/activate.
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
	response.OK(c, "Supplier activated successfully", result)
}

// Deactivate handles PATCH /suppliers/:id/deactivate.
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
	response.OK(c, "Supplier deactivated successfully", result)
}

// param binds and parses the :id path parameter, writing the error response
// itself and reporting whether the caller should continue.
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
