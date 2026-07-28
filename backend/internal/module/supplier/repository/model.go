// Package repository is the supplier module's persistence layer.
//
// LAYER RULE: the only package in this module permitted to import gorm or
// internal/shared/repository. It translates between the aggregate (unexported
// fields) and a persistence model GORM can map, exactly as the warehouse and
// product repositories do.
package repository

import (
	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/internal/module/supplier/entity"
	sharedentity "github.com/batokhehe/wms-saas/backend/internal/shared/entity"
)

// supplierModel is the persistence representation of the Supplier aggregate.
//
// entity.Supplier has unexported fields GORM cannot reflect over; exporting them
// would delete the encapsulation the aggregate rests on. So this model absorbs
// the ORM. The optional contact fields are nullable pointers so "unset" is
// distinct from "empty string"; the address fields are non-null with an empty
// default, mirroring the value object that always exists (possibly zero).
type supplierModel struct {
	sharedentity.BaseEntity

	CompanyID uuid.UUID `gorm:"type:uuid;not null;index"`

	Code string `gorm:"type:citext;not null"`
	Name string `gorm:"type:varchar(255);not null"`

	Email     *string `gorm:"type:citext"`
	Phone     *string `gorm:"type:varchar(32)"`
	TaxNumber *string `gorm:"column:tax_number;type:varchar(64)"`

	Address    string `gorm:"type:text;not null;default:''"`
	City       string `gorm:"type:varchar(128);not null;default:''"`
	Province   string `gorm:"type:varchar(128);not null;default:''"`
	Country    string `gorm:"type:varchar(128);not null;default:''"`
	PostalCode string `gorm:"column:postal_code;type:varchar(16);not null;default:''"`

	Status string `gorm:"type:varchar(16);not null;default:ACTIVE"`

	CreatedBy uuid.UUID `gorm:"type:uuid;not null"`
	UpdatedBy uuid.UUID `gorm:"type:uuid;not null"`
}

// TableName pins the table name so a struct rename cannot silently change the
// schema GORM targets.
func (supplierModel) TableName() string { return "suppliers" }

// toModel translates an aggregate into its persistence form, reading through the
// aggregate's getters — the only access anyone has.
func toModel(s *entity.Supplier) *supplierModel {
	address := s.Address()
	model := &supplierModel{
		CompanyID:  s.CompanyID(),
		Code:       s.Code().String(),
		Name:       s.Name(),
		Email:      optional(s.Email().String()),
		Phone:      optional(s.Phone().String()),
		TaxNumber:  optional(s.TaxNumber().String()),
		Address:    address.Street(),
		City:       address.City(),
		Province:   address.Province(),
		Country:    address.Country(),
		PostalCode: address.PostalCode(),
		Status:     s.Status().String(),
		CreatedBy:  s.CreatedBy(),
		UpdatedBy:  s.UpdatedBy(),
	}
	model.ID = s.ID()
	model.Version = s.Version()
	model.CreatedAt = s.CreatedAt()
	model.UpdatedAt = s.UpdatedAt()
	return model
}

// toDomain rebuilds an aggregate from a row via entity.Reconstitute, NOT the
// factory: loading a row is not a business event. Value-object construction
// errors are discarded (the database already enforced the constraints), matching
// the warehouse and product modules; Reconstitute still rejects a structurally
// invalid aggregate.
func toDomain(model *supplierModel) *entity.Supplier {
	code, _ := entity.NewSupplierCode(model.Code)

	email := entity.NoEmail()
	if model.Email != nil {
		if built, err := entity.NewEmail(*model.Email); err == nil {
			email = built
		}
	}
	phone := entity.NoPhone()
	if model.Phone != nil {
		if built, err := entity.NewPhone(*model.Phone); err == nil {
			phone = built
		}
	}
	tax := entity.NoTaxNumber()
	if model.TaxNumber != nil {
		if built, err := entity.NewTaxNumber(*model.TaxNumber); err == nil {
			tax = built
		}
	}
	address, _ := entity.NewAddress(model.Address, model.City, model.Province, model.Country, model.PostalCode)

	supplier, _ := entity.Reconstitute(
		model.ID,
		model.CompanyID,
		code,
		model.Name,
		email,
		phone,
		tax,
		address,
		entity.Status(model.Status),
		model.Version,
		model.CreatedBy,
		model.UpdatedBy,
		model.CreatedAt,
		model.UpdatedAt,
	)
	return supplier
}

// toDomainSlice translates a page of rows.
func toDomainSlice(models []supplierModel) []*entity.Supplier {
	result := make([]*entity.Supplier, 0, len(models))
	for i := range models {
		result = append(result, toDomain(&models[i]))
	}
	return result
}

// optional renders a value-object string as a nullable column value: nil when
// empty, so an unset email is NULL rather than an empty string.
func optional(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}
