// Package handler is the tenancy module's HTTP layer.
//
// LAYER RULE: bind input, call the service, shape the response, return. No
// business logic, no persistence, no c.JSON.
//
// Note what is absent from every method below: any mention of CompanyID. The
// active tenant comes from the RequestContext, which the company middleware
// populated — so no handler filters by company, as the repository rules
// require. A handler that could name a tenant would be a handler that could
// name someone else's.
package handler

import (
	"github.com/gin-gonic/gin"

	"github.com/batokhehe/wms-saas/backend/internal/module/tenancy/dto"
	"github.com/batokhehe/wms-saas/backend/internal/module/tenancy/service"
	"github.com/batokhehe/wms-saas/backend/internal/shared/appcontext"
	"github.com/batokhehe/wms-saas/backend/internal/shared/response"
	"github.com/batokhehe/wms-saas/backend/internal/shared/validator"
)

// CompanyHandler exposes the company use cases over HTTP.
type CompanyHandler struct {
	service *service.CompanyService
}

// NewCompanyHandler wires the handler to its service.
func NewCompanyHandler(svc *service.CompanyService) *CompanyHandler {
	return &CompanyHandler{service: svc}
}

// Create handles POST /companies.
func (h *CompanyHandler) Create(c *gin.Context) {
	var req dto.CreateCompanyRequest
	if err := validator.BindJSON(c, &req); err != nil {
		response.Error(c, err)
		return
	}

	result, err := h.service.Create(appcontext.Context(c), req)
	if err != nil {
		response.Error(c, err)
		return
	}

	response.Created(c, "Company created successfully", result)
}

// List handles GET /companies.
func (h *CompanyHandler) List(c *gin.Context) {
	var query dto.ListCompaniesQuery
	if err := validator.BindQuery(c, &query); err != nil {
		response.Error(c, err)
		return
	}

	page, err := h.service.List(appcontext.Context(c), query)
	if err != nil {
		response.Error(c, err)
		return
	}

	response.Page(c, "Companies retrieved successfully", page)
}

// Current handles GET /companies/current.
func (h *CompanyHandler) Current(c *gin.Context) {
	result, err := h.service.Current(appcontext.Context(c))
	if err != nil {
		response.Error(c, err)
		return
	}

	response.OK(c, "Current company retrieved successfully", result)
}

// Switch handles POST /companies/switch.
func (h *CompanyHandler) Switch(c *gin.Context) {
	var req dto.SwitchCompanyRequest
	if err := validator.BindJSON(c, &req); err != nil {
		response.Error(c, err)
		return
	}

	result, err := h.service.Switch(appcontext.Context(c), req)
	if err != nil {
		response.Error(c, err)
		return
	}

	// The client must send this company id in X-Company-ID on subsequent
	// requests. It is echoed in the header as well as the body so a client can
	// pick it up from either.
	c.Header("X-Company-ID", result.Company.ID.String())

	response.OK(c, "Active company switched successfully", result)
}

// Get handles GET /companies/:id.
func (h *CompanyHandler) Get(c *gin.Context) {
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

	result, err := h.service.Get(appcontext.Context(c), id)
	if err != nil {
		response.Error(c, err)
		return
	}

	response.OK(c, "Company retrieved successfully", result)
}

// Update handles PUT /companies/:id.
func (h *CompanyHandler) Update(c *gin.Context) {
	var param dto.IDParam
	if err := validator.BindURI(c, &param); err != nil {
		response.Error(c, err)
		return
	}

	var req dto.UpdateCompanyRequest
	if err := validator.BindJSON(c, &req); err != nil {
		response.Error(c, err)
		return
	}

	id, err := param.UUID()
	if err != nil {
		response.Error(c, err)
		return
	}

	result, err := h.service.Update(appcontext.Context(c), id, req)
	if err != nil {
		response.Error(c, err)
		return
	}

	response.OK(c, "Company updated successfully", result)
}

// Delete handles DELETE /companies/:id.
func (h *CompanyHandler) Delete(c *gin.Context) {
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

	if err := h.service.Delete(appcontext.Context(c), id); err != nil {
		response.Error(c, err)
		return
	}

	response.OK(c, "Company deleted successfully", nil)
}
