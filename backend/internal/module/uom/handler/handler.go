package handler

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/internal/module/uom/dto"
	"github.com/batokhehe/wms-saas/backend/internal/module/uom/repository"
	"github.com/batokhehe/wms-saas/backend/internal/module/uom/service"
	"github.com/batokhehe/wms-saas/backend/internal/shared/appcontext"
	"github.com/batokhehe/wms-saas/backend/internal/shared/response"
	"github.com/batokhehe/wms-saas/backend/internal/shared/validator"
)

type Handler struct {
	service *service.Service
}

func New(svc *service.Service) *Handler {
	return &Handler{service: svc}
}

func (h *Handler) Create(c *gin.Context) {
	var req dto.CreateUOMRequest
	if err := validator.BindJSON(c, &req); err != nil {
		response.Error(c, err)
		return
	}

	result, err := h.service.CreateUOM(appcontext.Context(c), req)
	if err != nil {
		response.Error(c, err)
		return
	}

	response.Created(c, "UOM created successfully", result)
}

func (h *Handler) List(c *gin.Context) {
	var query dto.ListUOMQuery
	if err := validator.BindQuery(c, &query); err != nil {
		response.Error(c, err)
		return
	}

	filter := repository.ListFilter{
		Paging: query.Request,
		Status: query.Status,
	}

	page, err := h.service.ListUOM(appcontext.Context(c), filter)
	if err != nil {
		response.Error(c, err)
		return
	}

	response.Page(c, "UOMs retrieved successfully", page)
}

func (h *Handler) Get(c *gin.Context) {
	id, ok := h.param(c)
	if !ok {
		return
	}

	result, err := h.service.GetUOMByID(appcontext.Context(c), id)
	if err != nil {
		response.Error(c, err)
		return
	}

	response.OK(c, "UOM retrieved successfully", result)
}

func (h *Handler) Update(c *gin.Context) {
	id, ok := h.param(c)
	if !ok {
		return
	}

	var req dto.UpdateUOMRequest
	if err := validator.BindJSON(c, &req); err != nil {
		response.Error(c, err)
		return
	}

	result, err := h.service.UpdateUOM(appcontext.Context(c), id, req)
	if err != nil {
		response.Error(c, err)
		return
	}

	response.OK(c, "UOM updated successfully", result)
}

func (h *Handler) Activate(c *gin.Context) {
	id, ok := h.param(c)
	if !ok {
		return
	}

	result, err := h.service.ActivateUOM(appcontext.Context(c), id)
	if err != nil {
		response.Error(c, err)
		return
	}

	response.OK(c, "UOM activated successfully", result)
}

func (h *Handler) Deactivate(c *gin.Context) {
	id, ok := h.param(c)
	if !ok {
		return
	}

	result, err := h.service.DeactivateUOM(appcontext.Context(c), id)
	if err != nil {
		response.Error(c, err)
		return
	}

	response.OK(c, "UOM deactivated successfully", result)
}

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
