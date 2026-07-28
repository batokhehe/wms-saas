// Package repository is the customer module's persistence layer.
//
// LAYER RULE: the only package in this module permitted to import gorm or
// internal/shared/repository. It translates between the aggregate (unexported
// fields) and a persistence model GORM can map — the structural sibling of the
// supplier repository.
package repository

import (
	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/internal/module/customer/entity"
	sharedentity "github.com/batokhehe/wms-saas/backend/internal/shared/entity"
)

// customerModel is the persistence representation of the Customer aggregate.
type customerModel struct {
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
func (customerModel) TableName() string { return "customers" }

// toModel translates an aggregate into its persistence form.
func toModel(c *entity.Customer) *customerModel {
	address := c.Address()
	model := &customerModel{
		CompanyID:  c.CompanyID(),
		Code:       c.Code().String(),
		Name:       c.Name(),
		Email:      optional(c.Email().String()),
		Phone:      optional(c.Phone().String()),
		TaxNumber:  optional(c.TaxNumber().String()),
		Address:    address.Street(),
		City:       address.City(),
		Province:   address.Province(),
		Country:    address.Country(),
		PostalCode: address.PostalCode(),
		Status:     c.Status().String(),
		CreatedBy:  c.CreatedBy(),
		UpdatedBy:  c.UpdatedBy(),
	}
	model.ID = c.ID()
	model.Version = c.Version()
	model.CreatedAt = c.CreatedAt()
	model.UpdatedAt = c.UpdatedAt()
	return model
}

// toDomain rebuilds an aggregate from a row via entity.Reconstitute. Value-object
// construction errors are discarded (the database already enforced the
// constraints); Reconstitute still rejects a structurally invalid aggregate.
func toDomain(model *customerModel) *entity.Customer {
	code, _ := entity.NewCustomerCode(model.Code)

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

	customer, _ := entity.Reconstitute(
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
	return customer
}

// toDomainSlice translates a page of rows.
func toDomainSlice(models []customerModel) []*entity.Customer {
	result := make([]*entity.Customer, 0, len(models))
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
