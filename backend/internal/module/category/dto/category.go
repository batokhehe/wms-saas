package dto

import (
	"time"

	"github.com/batokhehe/wms-saas/backend/internal/shared/pagination"
	"github.com/google/uuid"
)

type CategoryResponse struct {
	ID          uuid.UUID `json:"id"`
	Code        string    `json:"code"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type CreateCategoryRequest struct {
	Code        string `json:"code" binding:"required"`
	Name        string `json:"name" binding:"required"`
	Description string `json:"description"`
}

type UpdateCategoryRequest struct {
	Name        string `json:"name" binding:"required"`
	Description string `json:"description"`
}

type IDParam struct {
	ID uuid.UUID `uri:"id" binding:"required,uuid"`
}

func (p IDParam) UUID() (uuid.UUID, error) { return p.ID, nil }

type ListCategoryQuery struct {
	pagination.Request
	Status string `form:"status"`
}
