// Package handler is the goods-receipt module's HTTP layer.
//
// LAYER RULE: bind input, call the service, shape the response, return. No method
// contains a permission check, a status comparison or a business rule.
package handler

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/internal/module/goodsreceipt/dto"
	"github.com/batokhehe/wms-saas/backend/internal/module/goodsreceipt/service"
	"github.com/batokhehe/wms-saas/backend/internal/shared/appcontext"
	"github.com/batokhehe/wms-saas/backend/internal/shared/response"
	"github.com/batokhehe/wms-saas/backend/internal/shared/validator"
)

// Handler exposes the goods-receipt use cases over HTTP.
type Handler struct {
	service *service.Service
}

// New wires the handler to its service.
func New(svc *service.Service) *Handler { return &Handler{service: svc} }

// List handles GET /goods-receipts.
func (h *Handler) List(c *gin.Context) {
	var query dto.ListGoodsReceiptsQuery
	if err := validator.BindQuery(c, &query); err != nil {
		response.Error(c, err)
		return
	}
	page, err := h.service.List(appcontext.Context(c), query)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Page(c, "Goods receipts retrieved successfully", page)
}

// Get handles GET /goods-receipts/:id.
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
	response.OK(c, "Goods receipt retrieved successfully", result)
}

// Create handles POST /goods-receipts.
func (h *Handler) Create(c *gin.Context) {
	var req dto.CreateGoodsReceiptRequest
	if err := validator.BindJSON(c, &req); err != nil {
		response.Error(c, err)
		return
	}
	result, err := h.service.Create(appcontext.Context(c), req)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Created(c, "Goods receipt created successfully", result)
}

// Update handles PUT /goods-receipts/:id.
func (h *Handler) Update(c *gin.Context) {
	id, err := h.param(c)
	if err != nil {
		response.Error(c, err)
		return
	}
	var req dto.UpdateGoodsReceiptRequest
	if err := validator.BindJSON(c, &req); err != nil {
		response.Error(c, err)
		return
	}
	result, err := h.service.Update(appcontext.Context(c), id, req)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, "Goods receipt updated successfully", result)
}

// Confirm handles POST /goods-receipts/:id/confirm.
func (h *Handler) Confirm(c *gin.Context) {
	h.lifecycle(c, "Goods receipt confirmed successfully", func(id uuid.UUID) (dto.GoodsReceiptResponse, error) {
		return h.service.Confirm(appcontext.Context(c), id)
	})
}

// Receive handles POST /goods-receipts/:id/receive.
func (h *Handler) Receive(c *gin.Context) {
	h.lifecycle(c, "Goods receipt received successfully", func(id uuid.UUID) (dto.GoodsReceiptResponse, error) {
		return h.service.Receive(appcontext.Context(c), id)
	})
}

// Cancel handles POST /goods-receipts/:id/cancel.
func (h *Handler) Cancel(c *gin.Context) {
	id, err := h.param(c)
	if err != nil {
		response.Error(c, err)
		return
	}
	// The body is optional: cancelling without a stated reason is allowed.
	var req dto.CancelGoodsReceiptRequest
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
	response.OK(c, "Goods receipt cancelled successfully", result)
}

// Delete handles DELETE /goods-receipts/:id, which removes a DRAFT.
func (h *Handler) Delete(c *gin.Context) {
	id, err := h.param(c)
	if err != nil {
		response.Error(c, err)
		return
	}
	if err := h.service.DeleteDraft(appcontext.Context(c), id); err != nil {
		response.Error(c, err)
		return
	}
	response.NoContent(c)
}

// lifecycle is the shared shape of the parameterless transition endpoints.
func (h *Handler) lifecycle(
	c *gin.Context, message string, call func(uuid.UUID) (dto.GoodsReceiptResponse, error),
) {
	id, err := h.param(c)
	if err != nil {
		response.Error(c, err)
		return
	}
	result, err := call(id)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, message, result)
}

// param binds and parses the :id path parameter.
func (h *Handler) param(c *gin.Context) (uuid.UUID, error) {
	var p dto.IDParam
	if err := validator.BindURI(c, &p); err != nil {
		return uuid.Nil, err
	}
	return p.UUID()
}
