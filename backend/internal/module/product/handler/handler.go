// Package handler is the product module's HTTP layer.
//
// LAYER RULE: bind input, call the service, shape the response, return.
//
// Note what NO method contains: a permission check, a status comparison, or any
// reference to a business rule. Authorisation is declared in route/route.go;
// business rules are in the aggregate. A handler that checked either would be
// duplicating a decision made elsewhere.
package handler

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/internal/module/product/dto"
	"github.com/batokhehe/wms-saas/backend/internal/module/product/service"
	"github.com/batokhehe/wms-saas/backend/internal/shared/appcontext"
	"github.com/batokhehe/wms-saas/backend/internal/shared/response"
	"github.com/batokhehe/wms-saas/backend/internal/shared/validator"
)

// Handler exposes the product use cases over HTTP.
type Handler struct {
	service *service.Service
}

// New wires the handler to its service.
func New(svc *service.Service) *Handler {
	return &Handler{service: svc}
}

// Create handles POST /products.
func (h *Handler) Create(c *gin.Context) {
	var req dto.CreateProductRequest
	if err := validator.BindJSON(c, &req); err != nil {
		response.Error(c, err)
		return
	}
	result, err := h.service.Create(appcontext.Context(c), req)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Created(c, "Product created successfully", result)
}

// List handles GET /products.
func (h *Handler) List(c *gin.Context) {
	var query dto.ListProductsQuery
	if err := validator.BindQuery(c, &query); err != nil {
		response.Error(c, err)
		return
	}
	page, err := h.service.List(appcontext.Context(c), query)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Page(c, "Products retrieved successfully", page)
}

// Get handles GET /products/:id.
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
	response.OK(c, "Product retrieved successfully", result)
}

// Update handles PUT /products/:id.
func (h *Handler) Update(c *gin.Context) {
	id, ok := h.param(c)
	if !ok {
		return
	}
	var req dto.UpdateProductRequest
	if err := validator.BindJSON(c, &req); err != nil {
		response.Error(c, err)
		return
	}
	result, err := h.service.Update(appcontext.Context(c), id, req)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, "Product updated successfully", result)
}

// AssignCategory handles PATCH /products/:id/category.
func (h *Handler) AssignCategory(c *gin.Context) {
	id, ok := h.param(c)
	if !ok {
		return
	}
	var req dto.AssignCategoryRequest
	if err := validator.BindJSON(c, &req); err != nil {
		response.Error(c, err)
		return
	}
	result, err := h.service.AssignCategory(appcontext.Context(c), id, req)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, "Product category updated successfully", result)
}

// AssignBrand handles PATCH /products/:id/brand.
func (h *Handler) AssignBrand(c *gin.Context) {
	id, ok := h.param(c)
	if !ok {
		return
	}
	var req dto.AssignBrandRequest
	if err := validator.BindJSON(c, &req); err != nil {
		response.Error(c, err)
		return
	}
	result, err := h.service.AssignBrand(appcontext.Context(c), id, req)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, "Product brand updated successfully", result)
}

// SetMeasurements handles PATCH /products/:id/measurements.
func (h *Handler) SetMeasurements(c *gin.Context) {
	id, ok := h.param(c)
	if !ok {
		return
	}
	var req dto.SetMeasurementsRequest
	if err := validator.BindJSON(c, &req); err != nil {
		response.Error(c, err)
		return
	}
	result, err := h.service.SetMeasurements(appcontext.Context(c), id, req)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, "Product measurements updated successfully", result)
}

// SetShelfLife handles PATCH /products/:id/shelf-life.
func (h *Handler) SetShelfLife(c *gin.Context) {
	id, ok := h.param(c)
	if !ok {
		return
	}
	var req dto.SetShelfLifeRequest
	if err := validator.BindJSON(c, &req); err != nil {
		response.Error(c, err)
		return
	}
	result, err := h.service.SetShelfLife(appcontext.Context(c), id, req)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, "Product shelf life updated successfully", result)
}

// SetTracking handles PATCH /products/:id/tracking.
func (h *Handler) SetTracking(c *gin.Context) {
	id, ok := h.param(c)
	if !ok {
		return
	}
	var req dto.SetTrackingRequest
	if err := validator.BindJSON(c, &req); err != nil {
		response.Error(c, err)
		return
	}
	result, err := h.service.SetTracking(appcontext.Context(c), id, req)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, "Product tracking updated successfully", result)
}

// AddBarcode handles POST /products/:id/barcodes.
func (h *Handler) AddBarcode(c *gin.Context) {
	id, ok := h.param(c)
	if !ok {
		return
	}
	var req dto.AddBarcodeRequest
	if err := validator.BindJSON(c, &req); err != nil {
		response.Error(c, err)
		return
	}
	result, err := h.service.AddBarcode(appcontext.Context(c), id, req)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, "Barcode added successfully", result)
}

// SetPrimaryBarcode handles PATCH /products/:id/barcodes/primary.
func (h *Handler) SetPrimaryBarcode(c *gin.Context) {
	id, ok := h.param(c)
	if !ok {
		return
	}
	var req dto.SetPrimaryBarcodeRequest
	if err := validator.BindJSON(c, &req); err != nil {
		response.Error(c, err)
		return
	}
	result, err := h.service.SetPrimaryBarcode(appcontext.Context(c), id, req)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, "Primary barcode updated successfully", result)
}

// RemoveBarcode handles DELETE /products/:id/barcodes/:barcode.
func (h *Handler) RemoveBarcode(c *gin.Context) {
	var param dto.BarcodeParam
	if err := validator.BindURI(c, &param); err != nil {
		response.Error(c, err)
		return
	}
	productID, err := param.ProductID()
	if err != nil {
		response.Error(c, err)
		return
	}
	result, err := h.service.RemoveBarcode(appcontext.Context(c), productID, param.Barcode)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, "Barcode removed successfully", result)
}

// AddUOM handles POST /products/:id/uoms.
func (h *Handler) AddUOM(c *gin.Context) {
	id, ok := h.param(c)
	if !ok {
		return
	}
	var req dto.AddUOMRequest
	if err := validator.BindJSON(c, &req); err != nil {
		response.Error(c, err)
		return
	}
	result, err := h.service.AddUOM(appcontext.Context(c), id, req)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, "Unit of measure added successfully", result)
}

// RemoveUOM handles DELETE /products/:id/uoms/:uomId.
func (h *Handler) RemoveUOM(c *gin.Context) {
	var param dto.UOMParam
	if err := validator.BindURI(c, &param); err != nil {
		response.Error(c, err)
		return
	}
	productID, uomID, err := param.IDs()
	if err != nil {
		response.Error(c, err)
		return
	}
	result, err := h.service.RemoveUOM(appcontext.Context(c), productID, uomID)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, "Unit of measure removed successfully", result)
}

// Activate handles PATCH /products/:id/activate.
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
	response.OK(c, "Product activated successfully", result)
}

// Deactivate handles PATCH /products/:id/deactivate.
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
	response.OK(c, "Product deactivated successfully", result)
}

// Discontinue handles PATCH /products/:id/discontinue.
func (h *Handler) Discontinue(c *gin.Context) {
	id, ok := h.param(c)
	if !ok {
		return
	}
	result, err := h.service.Discontinue(appcontext.Context(c), id)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, "Product discontinued successfully", result)
}

// param binds and parses the :id path parameter. It writes the error response
// itself and reports whether the caller should continue, so the two-step
// bind-then-parse does not appear in every handler.
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
