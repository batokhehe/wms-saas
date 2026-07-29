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

type Handler struct{ service *service.Service }

func New(svc *service.Service) *Handler { return &Handler{service: svc} }

func (h *Handler) Create(c *gin.Context) {
	var req dto.CreateProductRequest
	if err := validator.BindJSON(c, &req); err != nil {
		response.Error(c, err)
		return
	}
	res, err := h.service.CreateProduct(appcontext.Context(c), req)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Created(c, "Product created", res)
}
func (h *Handler) List(c *gin.Context) {
	var query dto.ListProductQuery
	if err := validator.BindQuery(c, &query); err != nil {
		response.Error(c, err)
		return
	}
	p, err := h.service.ListProducts(appcontext.Context(c), query)
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
	res, err := h.service.GetProductByID(appcontext.Context(c), id)
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
	var req dto.UpdateProductRequest
	if err := validator.BindJSON(c, &req); err != nil {
		response.Error(c, err)
		return
	}
	res, err := h.service.UpdateProduct(appcontext.Context(c), id, req)
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
	res, err := h.service.ActivateProduct(appcontext.Context(c), id)
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
	res, err := h.service.DeactivateProduct(appcontext.Context(c), id)
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
	if err := h.service.ArchiveProduct(appcontext.Context(c), id); err != nil {
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
