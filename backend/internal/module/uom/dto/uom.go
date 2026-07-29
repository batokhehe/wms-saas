package dto

import (
	"time"

	"github.com/batokhehe/wms-saas/backend/internal/shared/pagination"
	"github.com/google/uuid"
)

type UOMResponse struct {
	ID          uuid.UUID `json:"id"`
	Code        string    `json:"code"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type CreateUOMRequest struct {
	Code        string `json:"code"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type UpdateUOMRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type IDParam struct {
	ID uuid.UUID `uri:"id" binding:"required,uuid"`
}

func (p IDParam) UUID() (uuid.UUID, error) { return p.ID, nil }

type ListUOMQuery struct {
	pagination.Request
	Status string `form:"status"`
}
