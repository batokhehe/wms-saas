// Package handler is the inventory-ledger module's HTTP layer.
//
// LAYER RULE: bind input, call the service, shape the response, return.
//
// Every endpoint here is a READ. The ledger has no write endpoint because
// entries are produced by the Inventory module through the integration seam, not
// by a client — exposing a POST would let a caller fabricate history.
package handler

import (
	"github.com/gin-gonic/gin"

	"github.com/batokhehe/wms-saas/backend/internal/module/inventoryledger/dto"
	"github.com/batokhehe/wms-saas/backend/internal/module/inventoryledger/service"
	"github.com/batokhehe/wms-saas/backend/internal/shared/appcontext"
	"github.com/batokhehe/wms-saas/backend/internal/shared/response"
	"github.com/batokhehe/wms-saas/backend/internal/shared/validator"
)

// Handler exposes the ledger read use cases over HTTP.
type Handler struct {
	service *service.Service
}

// New wires the handler to its service.
func New(svc *service.Service) *Handler {
	return &Handler{service: svc}
}

// List handles GET /inventory-ledger.
//
// Supports pagination, sorting, a half-open date range, and filtering by movement
// type, product, warehouse, location, position and document reference.
func (h *Handler) List(c *gin.Context) {
	var query dto.ListLedgerQuery
	if err := validator.BindQuery(c, &query); err != nil {
		response.Error(c, err)
		return
	}
	page, err := h.service.ListLedger(appcontext.Context(c), query)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Page(c, "Inventory ledger retrieved successfully", page)
}

// Get handles GET /inventory-ledger/:id.
func (h *Handler) Get(c *gin.Context) {
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

	result, err := h.service.GetLedger(appcontext.Context(c), id)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, "Ledger entry retrieved successfully", result)
}

// ListByPosition handles GET /inventory-ledger/position/:positionId — the
// movement history of one stock position, newest first.
func (h *Handler) ListByPosition(c *gin.Context) {
	var param dto.PositionParam
	if err := validator.BindURI(c, &param); err != nil {
		response.Error(c, err)
		return
	}
	positionID, err := param.UUID()
	if err != nil {
		response.Error(c, err)
		return
	}

	var query dto.ListLedgerQuery
	if err := validator.BindQuery(c, &query); err != nil {
		response.Error(c, err)
		return
	}

	page, err := h.service.ListByPosition(appcontext.Context(c), positionID, query.Request)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Page(c, "Position ledger retrieved successfully", page)
}
