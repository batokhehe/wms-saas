// Package handler is the customer module's HTTP layer.
//
// LAYER RULE: bind input, call the service, shape the response, return.
package handler

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/internal/module/customer/dto"
	"github.com/batokhehe/wms-saas/backend/internal/module/customer/service"
	"github.com/batokhehe/wms-saas/backend/internal/shared/appcontext"
	"github.com/batokhehe/wms-saas/backend/internal/shared/response"
	"github.com/batokhehe/wms-saas/backend/internal/shared/validator"
)

// Handler exposes the customer use cases over HTTP.
type Handler struct {
	service *service.Service
}

// New wires the handler to its service.
func New(svc *service.Service) *Handler {
	return &Handler{service: svc}
}

// Create handles POST /customers.
func (h *Handler) Create(c *gin.Context) {
	var req dto.CreateCustomerRequest
	if err := validator.BindJSON(c, &req); err != nil {
		response.Error(c, err)
		return
	}
	result, err := h.service.Create(appcontext.Context(c), req)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Created(c, "Customer created successfully", result)
}

// List handles GET /customers.
func (h *Handler) List(c *gin.Context) {
	var query dto.ListCustomersQuery
	if err := validator.BindQuery(c, &query); err != nil {
		response.Error(c, err)
		return
	}
	page, err := h.service.List(appcontext.Context(c), query)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Page(c, "Customers retrieved successfully", page)
}

// Get handles GET /customers/:id.
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
	response.OK(c, "Customer retrieved successfully", result)
}

// Update handles PUT /customers/:id.
func (h *Handler) Update(c *gin.Context) {
	id, ok := h.param(c)
	if !ok {
		return
	}
	var req dto.UpdateCustomerRequest
	if err := validator.BindJSON(c, &req); err != nil {
		response.Error(c, err)
		return
	}
	result, err := h.service.Update(appcontext.Context(c), id, req)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, "Customer updated successfully", result)
}

// Activate handles PATCH /customers/:id/activate.
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
	response.OK(c, "Customer activated successfully", result)
}

// Deactivate handles PATCH /customers/:id/deactivate.
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
	response.OK(c, "Customer deactivated successfully", result)
}

// param binds and parses the :id path parameter.
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
