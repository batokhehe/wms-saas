// Package handler is the inventory module's HTTP layer.
//
// LAYER RULE: bind input, call the service, shape the response, return. No method
// contains a permission check, a status comparison or a business rule.
// Authorisation is declared in route/route.go; business rules are in the
// aggregate.
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

// Create handles POST /inventories.
func (h *Handler) Create(c *gin.Context) {
	var req dto.CreateInventoryRequest
	if err := validator.BindJSON(c, &req); err != nil {
		response.Error(c, err)
		return
	}
	result, err := h.service.CreateInventory(appcontext.Context(c), req)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Created(c, "Inventory created successfully", result)
}

// List handles GET /inventories.
func (h *Handler) List(c *gin.Context) {
	var query dto.ListInventoriesQuery
	if err := validator.BindQuery(c, &query); err != nil {
		response.Error(c, err)
		return
	}
	page, err := h.service.ListInventory(appcontext.Context(c), query)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Page(c, "Inventory retrieved successfully", page)
}

// Get handles GET /inventories/:id.
func (h *Handler) Get(c *gin.Context) {
	id, ok := h.param(c)
	if !ok {
		return
	}
	result, err := h.service.GetInventory(appcontext.Context(c), id)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, "Inventory retrieved successfully", result)
}

// Increase handles POST /inventories/:id/increase.
func (h *Handler) Increase(c *gin.Context) {
	h.quantityOp(c, "Inventory increased successfully", h.service.IncreaseInventory)
}

// Decrease handles POST /inventories/:id/decrease.
func (h *Handler) Decrease(c *gin.Context) {
	h.quantityOp(c, "Inventory decreased successfully", h.service.DecreaseInventory)
}

// Reserve handles POST /inventories/:id/reserve.
func (h *Handler) Reserve(c *gin.Context) {
	h.quantityOp(c, "Inventory reserved successfully", h.service.ReserveInventory)
}

// Release handles POST /inventories/:id/release.
func (h *Handler) Release(c *gin.Context) {
	h.quantityOp(c, "Reservation released successfully", h.service.ReleaseReservation)
}

// TransferOut handles POST /inventories/:id/transfer-out.
func (h *Handler) TransferOut(c *gin.Context) {
	h.quantityOp(c, "Inventory transferred out successfully", h.service.TransferOut)
}

// TransferIn handles POST /inventories/:id/transfer-in.
func (h *Handler) TransferIn(c *gin.Context) {
	h.quantityOp(c, "Inventory transferred in successfully", h.service.TransferIn)
}

// Adjust handles POST /inventories/:id/adjust.
func (h *Handler) Adjust(c *gin.Context) {
	id, ok := h.param(c)
	if !ok {
		return
	}
	var req dto.AdjustInventoryRequest
	if err := validator.BindJSON(c, &req); err != nil {
		response.Error(c, err)
		return
	}
	result, err := h.service.AdjustInventory(appcontext.Context(c), id, req)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, "Inventory adjusted successfully", result)
}

// CycleCount handles POST /inventories/:id/cycle-count.
func (h *Handler) CycleCount(c *gin.Context) {
	id, ok := h.param(c)
	if !ok {
		return
	}
	var req dto.CycleCountRequest
	if err := validator.BindJSON(c, &req); err != nil {
		response.Error(c, err)
		return
	}
	result, err := h.service.CompleteCycleCount(appcontext.Context(c), id, req)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, "Cycle count completed successfully", result)
}

// Lock handles POST /inventories/:id/lock.
func (h *Handler) Lock(c *gin.Context) {
	id, ok := h.param(c)
	if !ok {
		return
	}
	result, err := h.service.LockInventory(appcontext.Context(c), id)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, "Inventory locked successfully", result)
}

// Unlock handles POST /inventories/:id/unlock.
func (h *Handler) Unlock(c *gin.Context) {
	id, ok := h.param(c)
	if !ok {
		return
	}
	result, err := h.service.UnlockInventory(appcontext.Context(c), id)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, "Inventory unlocked successfully", result)
}

// quantityOp is the shared shape of every endpoint whose body is a single
// movement amount. It binds the amount, calls the operation, and writes the
// response, so the six of them do not repeat the same six lines.
func (h *Handler) quantityOp(
	c *gin.Context,
	message string,
	op func(ctx context.Context, id uuid.UUID, req dto.QuantityRequest) (dto.InventoryResponse, error),
) {
	id, ok := h.param(c)
	if !ok {
		return
	}
	var req dto.QuantityRequest
	if err := validator.BindJSON(c, &req); err != nil {
		response.Error(c, err)
		return
	}
	result, err := op(appcontext.Context(c), id, req)
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
