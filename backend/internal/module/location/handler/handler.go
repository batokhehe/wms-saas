// Package handler is the location module's HTTP layer.
//
// LAYER RULE: bind input, call the service, shape the response, return.
//
// Note what NO method contains: a permission check, a status comparison, or any
// reference to a business rule. Authorisation is declared in route/route.go;
// business rules are in the aggregate.
package handler

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/internal/module/location/dto"
	"github.com/batokhehe/wms-saas/backend/internal/module/location/service"
	"github.com/batokhehe/wms-saas/backend/internal/shared/appcontext"
	"github.com/batokhehe/wms-saas/backend/internal/shared/response"
	"github.com/batokhehe/wms-saas/backend/internal/shared/validator"
)

// Handler exposes the location use cases over HTTP.
type Handler struct {
	service *service.Service
}

// New wires the handler to its service.
func New(svc *service.Service) *Handler {
	return &Handler{service: svc}
}

// Create handles POST /locations.
func (h *Handler) Create(c *gin.Context) {
	var req dto.CreateLocationRequest
	if err := validator.BindJSON(c, &req); err != nil {
		response.Error(c, err)
		return
	}

	result, err := h.service.Create(appcontext.Context(c), req)
	if err != nil {
		response.Error(c, err)
		return
	}

	response.Created(c, "Storage location created successfully", result)
}

// List handles GET /locations.
func (h *Handler) List(c *gin.Context) {
	var query dto.ListLocationsQuery
	if err := validator.BindQuery(c, &query); err != nil {
		response.Error(c, err)
		return
	}

	page, err := h.service.List(appcontext.Context(c), query)
	if err != nil {
		response.Error(c, err)
		return
	}

	response.Page(c, "Storage locations retrieved successfully", page)
}

// Get handles GET /locations/:id.
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

	response.OK(c, "Storage location retrieved successfully", result)
}

// GetByBarcode handles GET /locations/barcode/:barcode.
//
// A distinct route rather than a query parameter on the list endpoint, because
// a scan resolves to exactly one location — returning a paginated collection
// for it would make every scanner client unwrap a page to find one item.
func (h *Handler) GetByBarcode(c *gin.Context) {
	barcode := c.Param("barcode")

	result, err := h.service.GetByBarcode(appcontext.Context(c), barcode)
	if err != nil {
		response.Error(c, err)
		return
	}

	response.OK(c, "Storage location retrieved successfully", result)
}

// Update handles PUT /locations/:id.
func (h *Handler) Update(c *gin.Context) {
	id, ok := h.param(c)
	if !ok {
		return
	}

	var req dto.UpdateLocationRequest
	if err := validator.BindJSON(c, &req); err != nil {
		response.Error(c, err)
		return
	}

	result, err := h.service.Update(appcontext.Context(c), id, req)
	if err != nil {
		response.Error(c, err)
		return
	}

	response.OK(c, "Storage location updated successfully", result)
}

// ChangeCapacity handles PATCH /locations/:id/capacity.
func (h *Handler) ChangeCapacity(c *gin.Context) {
	id, ok := h.param(c)
	if !ok {
		return
	}

	var req dto.ChangeCapacityRequest
	if err := validator.BindJSON(c, &req); err != nil {
		response.Error(c, err)
		return
	}

	result, err := h.service.ChangeCapacity(appcontext.Context(c), id, req)
	if err != nil {
		response.Error(c, err)
		return
	}

	response.OK(c, "Storage location capacity updated successfully", result)
}

// AssignBarcode handles PATCH /locations/:id/barcode.
func (h *Handler) AssignBarcode(c *gin.Context) {
	id, ok := h.param(c)
	if !ok {
		return
	}

	var req dto.AssignBarcodeRequest
	if err := validator.BindJSON(c, &req); err != nil {
		response.Error(c, err)
		return
	}

	result, err := h.service.AssignBarcode(appcontext.Context(c), id, req)
	if err != nil {
		response.Error(c, err)
		return
	}

	response.OK(c, "Storage location barcode assigned successfully", result)
}

// Activate handles PATCH /locations/:id/activate.
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

	response.OK(c, "Storage location activated successfully", result)
}

// Deactivate handles PATCH /locations/:id/deactivate.
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

	response.OK(c, "Storage location deactivated successfully", result)
}

// Lock handles PATCH /locations/:id/lock.
func (h *Handler) Lock(c *gin.Context) {
	id, ok := h.param(c)
	if !ok {
		return
	}

	var req dto.LockLocationRequest
	if err := validator.BindJSON(c, &req); err != nil {
		response.Error(c, err)
		return
	}

	result, err := h.service.Lock(appcontext.Context(c), id, req)
	if err != nil {
		response.Error(c, err)
		return
	}

	response.OK(c, "Storage location locked successfully", result)
}

// Unlock handles PATCH /locations/:id/unlock.
func (h *Handler) Unlock(c *gin.Context) {
	id, ok := h.param(c)
	if !ok {
		return
	}

	result, err := h.service.Unlock(appcontext.Context(c), id)
	if err != nil {
		response.Error(c, err)
		return
	}

	response.OK(c, "Storage location unlocked successfully", result)
}

// StartMaintenance handles PATCH /locations/:id/maintenance.
func (h *Handler) StartMaintenance(c *gin.Context) {
	id, ok := h.param(c)
	if !ok {
		return
	}

	result, err := h.service.StartMaintenance(appcontext.Context(c), id)
	if err != nil {
		response.Error(c, err)
		return
	}

	response.OK(c, "Storage location placed under maintenance", result)
}

// Archive handles DELETE /locations/:id.
//
// A soft delete: the row is retained because future stock movements will
// reference it forever. There is no separate archive route here — unlike
// Warehouse, which offers both — because a location has no operator-facing
// "archive" vocabulary; retiring a bin is simply removing it from the layout.
func (h *Handler) Archive(c *gin.Context) {
	id, ok := h.param(c)
	if !ok {
		return
	}

	if err := h.service.Archive(appcontext.Context(c), id); err != nil {
		response.Error(c, err)
		return
	}

	response.OK(c, "Storage location archived successfully", nil)
}

// param binds and parses the :id path parameter.
//
// It writes the error response itself and reports whether the caller should
// continue, so the two-step bind-then-parse does not appear in ten handlers.
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
