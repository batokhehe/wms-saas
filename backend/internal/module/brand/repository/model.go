package repository

import sharedentity "github.com/batokhehe/wms-saas/backend/internal/shared/entity"
import "github.com/google/uuid"

type brandModel struct {
	sharedentity.BaseEntity
	CompanyID   uuid.UUID `gorm:"type:uuid;not null;index"`
	Code        string    `gorm:"type:citext;not null"`
	Name        string    `gorm:"type:varchar(255);not null"`
	Description string    `gorm:"type:text;not null;default:''"`
	Status      string    `gorm:"type:varchar(16);not null;default:ACTIVE"`
}

func (brandModel) TableName() string { return "brands" }
