// Package handler is the inventory module's HTTP layer.
//
// LAYER RULE: bind input, call the service, shape the response, return. No method
// contains a permission check, a bucket comparison or a business rule.
// Authorisation is declared in route/route.go; the rules are in the aggregate.
package handler

import (
	"context"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/internal/module/inventory/dto"
	"github.com/batokhehe/wms-saas/backend/internal/module/inventory/service"
	"github.com/batokhehe/wms-saas/backend/internal/shared/appcontext"
	"github.com/batokhehe/wms-saas/backend/internal/shared/response"
	"github.com/batokhehe/wms-saas/backend/internal/shared/validator"
)

// Handler exposes the inventory use cases over HTTP.
type Handler struct {
	service *service.Service
}

// New wires the handler to its service.
func New(svc *service.Service) *Handler {
	return &Handler{service: svc}
}

// Receive handles POST /inventory-positions/receive. It is the one operation
// addressed by stock KEY rather than by position id, because the position may not
// exist yet.
func (h *Handler) Receive(c *gin.Context) {
	var req dto.ReceiveStockRequest
	if err := validator.BindJSON(c, &req); err != nil {
		response.Error(c, err)
		return
	}
	result, err := h.service.ReceiveStock(appcontext.Context(c), req)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Created(c, "Stock received successfully", result)
}

// Issue handles POST /inventory-positions/:id/issue.
func (h *Handler) Issue(c *gin.Context) {
	h.quantityOp(c, "Stock issued successfully", h.service.IssueStock)
}

// Reserve handles POST /inventory-positions/:id/reserve.
func (h *Handler) Reserve(c *gin.Context) {
	h.quantityOp(c, "Stock reserved successfully", h.service.ReserveStock)
}

// Release handles POST /inventory-positions/:id/release.
func (h *Handler) Release(c *gin.Context) {
	h.quantityOp(c, "Reservation released successfully", h.service.ReleaseReservation)
}

// Allocate handles POST /inventory-positions/:id/allocate.
func (h *Handler) Allocate(c *gin.Context) {
	h.quantityOp(c, "Stock allocated successfully", h.service.AllocateStock)
}

// Deallocate handles POST /inventory-positions/:id/deallocate.
func (h *Handler) Deallocate(c *gin.Context) {
	h.quantityOp(c, "Stock deallocated successfully", h.service.DeallocateStock)
}

// Quarantine handles POST /inventory-positions/:id/quarantine.
func (h *Handler) Quarantine(c *gin.Context) {
	h.quantityOp(c, "Stock moved to quarantine successfully", h.service.MoveToQuarantine)
}

// ReleaseQuarantine handles POST /inventory-positions/:id/release-quarantine.
func (h *Handler) ReleaseQuarantine(c *gin.Context) {
	h.quantityOp(c, "Stock released from quarantine successfully", h.service.ReleaseFromQuarantine)
}

// Transfer handles POST /inventory-positions/:id/transfer.
func (h *Handler) Transfer(c *gin.Context) {
	id, ok := h.param(c)
	if !ok {
		return
	}
	var req dto.TransferStockRequest
	if err := validator.BindJSON(c, &req); err != nil {
		response.Error(c, err)
		return
	}
	// The path identifies the origin, so the body cannot disagree with the URL.
	req.FromPositionID = id

	result, err := h.service.TransferStock(appcontext.Context(c), req)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, "Stock transferred successfully", result)
}

// Adjust handles POST /inventory-positions/:id/adjust.
func (h *Handler) Adjust(c *gin.Context) {
	id, ok := h.param(c)
	if !ok {
		return
	}
	var req dto.AdjustStockRequest
	if err := validator.BindJSON(c, &req); err != nil {
		response.Error(c, err)
		return
	}
	req.PositionID = id

	result, err := h.service.AdjustStock(appcontext.Context(c), req)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, "Stock adjusted successfully", result)
}

// Get handles GET /inventory-positions/:id.
func (h *Handler) Get(c *gin.Context) {
	id, ok := h.param(c)
	if !ok {
		return
	}
	result, err := h.service.GetInventoryPosition(appcontext.Context(c), id)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, "Inventory position retrieved successfully", result)
}

// List handles GET /inventory-positions.
func (h *Handler) List(c *gin.Context) {
	var query dto.ListPositionsQuery
	if err := validator.BindQuery(c, &query); err != nil {
		response.Error(c, err)
		return
	}
	page, err := h.service.ListInventoryPositions(appcontext.Context(c), query)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Page(c, "Inventory positions retrieved successfully", page)
}

// quantityOp is the shared shape of every endpoint whose body is a single
// movement amount against the position named in the path. It binds the amount,
// calls the operation and writes the response, so the seven of them do not repeat
// the same lines.
func (h *Handler) quantityOp(
	c *gin.Context,
	message string,
	op func(ctx context.Context, req dto.PositionQuantityRequest) (dto.PositionResponse, error),
) {
	id, ok := h.param(c)
	if !ok {
		return
	}
	var req dto.PositionQuantityRequest
	if err := validator.BindJSON(c, &req); err != nil {
		response.Error(c, err)
		return
	}
	req.PositionID = id

	result, err := op(appcontext.Context(c), req)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, message, result)
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
