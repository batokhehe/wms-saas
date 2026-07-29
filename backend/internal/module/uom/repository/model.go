package repository

import sharedentity "github.com/batokhehe/wms-saas/backend/internal/shared/entity"

type uomModel struct {
	sharedentity.BaseEntity

	Code        string `gorm:"type:citext;not null"`
	Name        string `gorm:"type:varchar(255);not null"`
	Description string `gorm:"type:text;not null;default:''"`
	Status      string `gorm:"type:varchar(16);not null;default:ACTIVE"`
}

func (uomModel) TableName() string { return "uoms" }
