// Package handler is the purchase-order module's HTTP layer.
//
// LAYER RULE: bind input, call the service, shape the response, return. No method
// contains a permission check, a status comparison or a business rule.
package handler

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/internal/module/purchaseorder/dto"
	"github.com/batokhehe/wms-saas/backend/internal/module/purchaseorder/service"
	"github.com/batokhehe/wms-saas/backend/internal/shared/appcontext"
	"github.com/batokhehe/wms-saas/backend/internal/shared/response"
	"github.com/batokhehe/wms-saas/backend/internal/shared/validator"
)

// Handler exposes the purchase-order use cases over HTTP.
type Handler struct {
	service *service.Service
}

// New wires the handler to its service.
func New(svc *service.Service) *Handler { return &Handler{service: svc} }

// List handles GET /purchase-orders.
func (h *Handler) List(c *gin.Context) {
	var query dto.ListPurchaseOrdersQuery
	if err := validator.BindQuery(c, &query); err != nil {
		response.Error(c, err)
		return
	}
	page, err := h.service.List(appcontext.Context(c), query)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Page(c, "Purchase orders retrieved successfully", page)
}

// Get handles GET /purchase-orders/:id.
func (h *Handler) Get(c *gin.Context) {
	id, err := h.param(c)
	if err != nil {
		response.Error(c, err)
		return
	}
	result, err := h.service.Get(appcontext.Context(c), id)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, "Purchase order retrieved successfully", result)
}

// Create handles POST /purchase-orders.
func (h *Handler) Create(c *gin.Context) {
	var req dto.CreatePurchaseOrderRequest
	if err := validator.BindJSON(c, &req); err != nil {
		response.Error(c, err)
		return
	}
	result, err := h.service.Create(appcontext.Context(c), req)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Created(c, "Purchase order created successfully", result)
}

// Update handles PUT /purchase-orders/:id.
func (h *Handler) Update(c *gin.Context) {
	id, err := h.param(c)
	if err != nil {
		response.Error(c, err)
		return
	}
	var req dto.UpdatePurchaseOrderRequest
	if err := validator.BindJSON(c, &req); err != nil {
		response.Error(c, err)
		return
	}
	result, err := h.service.Update(appcontext.Context(c), id, req)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, "Purchase order updated successfully", result)
}

// Approve handles POST /purchase-orders/:id/approve.
func (h *Handler) Approve(c *gin.Context) {
	id, err := h.param(c)
	if err != nil {
		response.Error(c, err)
		return
	}
	result, err := h.service.Approve(appcontext.Context(c), id)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, "Purchase order approved successfully", result)
}

// Cancel handles POST /purchase-orders/:id/cancel.
func (h *Handler) Cancel(c *gin.Context) {
	id, err := h.param(c)
	if err != nil {
		response.Error(c, err)
		return
	}
	// The body is optional: cancelling without a stated reason is allowed.
	var req dto.CancelPurchaseOrderRequest
	if c.Request.ContentLength > 0 {
		if err := validator.BindJSON(c, &req); err != nil {
			response.Error(c, err)
			return
		}
	}
	result, err := h.service.Cancel(appcontext.Context(c), id, req)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, "Purchase order cancelled successfully", result)
}

// param binds and parses the :id path parameter.
func (h *Handler) param(c *gin.Context) (uuid.UUID, error) {
	var param dto.IDParam
	if err := validator.BindURI(c, &param); err != nil {
		return uuid.Nil, err
	}
	return param.UUID()
}
