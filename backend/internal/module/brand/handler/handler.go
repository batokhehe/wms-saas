package handler

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/internal/module/brand/dto"
	"github.com/batokhehe/wms-saas/backend/internal/module/brand/repository"
	"github.com/batokhehe/wms-saas/backend/internal/module/brand/service"
	"github.com/batokhehe/wms-saas/backend/internal/shared/appcontext"
	"github.com/batokhehe/wms-saas/backend/internal/shared/response"
	"github.com/batokhehe/wms-saas/backend/internal/shared/validator"
)

type Handler struct{ service *service.Service }

func New(svc *service.Service) *Handler { return &Handler{service: svc} }

func (h *Handler) Create(c *gin.Context) {
	var req dto.CreateBrandRequest
	if err := validator.BindJSON(c, &req); err != nil {
		response.Error(c, err)
		return
	}
	res, err := h.service.Create(appcontext.Context(c), req)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Created(c, "Brand created", res)
}
func (h *Handler) List(c *gin.Context) {
	var q dto.ListBrandQuery
	if err := validator.BindQuery(c, &q); err != nil {
		response.Error(c, err)
		return
	}
	p, err := h.service.List(appcontext.Context(c), repository.ListFilter{Paging: q.Request, Status: q.Status})
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Page(c, "List retrieved", p)
}
func (h *Handler) Get(c *gin.Context) {
	id, ok := h.param(c)
	if !ok {
		return
	}
	res, err := h.service.Get(appcontext.Context(c), id)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, "Retrieved", res)
}
func (h *Handler) Update(c *gin.Context) {
	id, ok := h.param(c)
	if !ok {
		return
	}
	var req dto.UpdateBrandRequest
	if err := validator.BindJSON(c, &req); err != nil {
		response.Error(c, err)
		return
	}
	res, err := h.service.Update(appcontext.Context(c), id, req)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, "Updated", res)
}
func (h *Handler) Activate(c *gin.Context) {
	id, ok := h.param(c)
	if !ok {
		return
	}
	res, err := h.service.Activate(appcontext.Context(c), id)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, "Activated", res)
}
func (h *Handler) Deactivate(c *gin.Context) {
	id, ok := h.param(c)
	if !ok {
		return
	}
	res, err := h.service.Deactivate(appcontext.Context(c), id)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, "Deactivated", res)
}
func (h *Handler) Archive(c *gin.Context) {
	id, ok := h.param(c)
	if !ok {
		return
	}
	if err := h.service.Archive(appcontext.Context(c), id); err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, "Archived", nil)
}
func (h *Handler) param(c *gin.Context) (uuid.UUID, bool) {
	var p dto.IDParam
	if err := validator.BindURI(c, &p); err != nil {
		response.Error(c, err)
		return uuid.Nil, false
	}
	id, _ := p.UUID()
	return id, true
}
