// Package handler is the module's HTTP layer.
//
// LAYER RULE: a handler does exactly four things — bind input, call the
// service, shape the response, return. It contains no business rules, performs
// no database access, and never calls c.JSON directly.
//
// Every method below follows the same five lines. That uniformity is the point:
// a reviewer can tell at a glance whether a handler is doing something it
// should not.
package handler

import (
	"github.com/gin-gonic/gin"

	"github.com/batokhehe/wms-saas/backend/internal/module/template/dto"
	"github.com/batokhehe/wms-saas/backend/internal/module/template/service"
	"github.com/batokhehe/wms-saas/backend/internal/shared/appcontext"
	"github.com/batokhehe/wms-saas/backend/internal/shared/response"
	"github.com/batokhehe/wms-saas/backend/internal/shared/validator"
)

// Handler exposes the module over HTTP.
type Handler struct {
	service *service.Service
}

// New wires the handler to its service.
func New(svc *service.Service) *Handler {
	return &Handler{service: svc}
}

// Create handles POST /resources.
func (h *Handler) Create(c *gin.Context) {
	var req dto.CreateRequest
	if err := validator.BindJSON(c, &req); err != nil {
		response.Error(c, err)
		return
	}

	result, err := h.service.Create(appcontext.Context(c), req)
	if err != nil {
		response.Error(c, err)
		return
	}

	response.Created(c, "Resource created successfully", result)
}

// Get handles GET /resources/:id.
func (h *Handler) Get(c *gin.Context) {
	var param dto.IDParam
	if err := validator.BindURI(c, &param); err != nil {
		response.Error(c, err)
		return
	}

	result, err := h.service.Get(appcontext.Context(c), param.ID)
	if err != nil {
		response.Error(c, err)
		return
	}

	response.OK(c, "Resource retrieved successfully", result)
}

// List handles GET /resources.
func (h *Handler) List(c *gin.Context) {
	var query dto.ListQuery
	if err := validator.BindQuery(c, &query); err != nil {
		response.Error(c, err)
		return
	}

	// Note the handler does not normalise paging. Apply enforces the sort
	// allow-list, which is a business rule about this endpoint, so it belongs
	// in the service alongside every other rule.
	page, err := h.service.List(appcontext.Context(c), query)
	if err != nil {
		response.Error(c, err)
		return
	}

	// response.Page writes items and pagination metadata together, so no module
	// computes total_pages differently.
	response.Page(c, "Resources retrieved successfully", page)
}

// Update handles PATCH /resources/:id.
func (h *Handler) Update(c *gin.Context) {
	var param dto.IDParam
	if err := validator.BindURI(c, &param); err != nil {
		response.Error(c, err)
		return
	}

	var req dto.UpdateRequest
	if err := validator.BindJSON(c, &req); err != nil {
		response.Error(c, err)
		return
	}

	result, err := h.service.Update(appcontext.Context(c), param.ID, req)
	if err != nil {
		response.Error(c, err)
		return
	}

	response.OK(c, "Resource updated successfully", result)
}

// Delete handles DELETE /resources/:id.
func (h *Handler) Delete(c *gin.Context) {
	var param dto.IDParam
	if err := validator.BindURI(c, &param); err != nil {
		response.Error(c, err)
		return
	}

	if err := h.service.Delete(appcontext.Context(c), param.ID); err != nil {
		response.Error(c, err)
		return
	}

	response.OK(c, "Resource deleted successfully", nil)
}
