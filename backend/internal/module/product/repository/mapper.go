package repository

import (
	"encoding/json"
	"time"

	"github.com/batokhehe/wms-saas/backend/internal/module/product/entity"
	sharedentity "github.com/batokhehe/wms-saas/backend/internal/shared/entity"
)

func toModel(p *entity.Product) *productModel {
	tc, _ := json.Marshal(p.TrackingConfig)
	ip, _ := json.Marshal(p.InventoryPolicy)
	dm, _ := json.Marshal(p.Dimensions)
	wg, _ := json.Marshal(p.Weight)
	vl, _ := json.Marshal(p.Volume)

	model := &productModel{
		BaseEntity:      sharedentity.BaseEntity{ID: p.ID(), Version: p.Version()},
		CompanyID:       p.CompanyID(),
		SKU:             string(p.SKU()),
		Name:            string(p.Name()),
		Description:     p.Description(),
		TrackingConfig:  tc,
		InventoryPolicy: ip,
		Dimensions:      dm,
		Weight:          wg,
		Volume:          vl,
		Status:          p.Status().String(),
	}
	if p.IsArchived() {
		model.DeletedAt.Time = *p.DeletedAt()
		model.DeletedAt.Valid = true
	}

	// Map child entities
	for _, b := range p.Barcodes() { // Need to add Barcodes() getter to entity
		model.Barcodes = append(model.Barcodes, barcodeModel{ID: b.ID, ProductID: p.ID(), Code: b.Code, Type: b.Type, IsPrimary: b.IsPrimary})
	}
	for _, u := range p.AlternateUOMs() { // Need to add AlternateUOMs() getter to entity
		model.AlternateUOMs = append(model.AlternateUOMs, uomModel{ID: u.ID, ProductID: p.ID(), UOMID: u.UOMID, Factor: u.Factor})
	}

	return model
}

func toDomain(model *productModel) *entity.Product {
	var tc entity.TrackingConfig
	json.Unmarshal(model.TrackingConfig, &tc)
	var ip entity.InventoryPolicy
	json.Unmarshal(model.InventoryPolicy, &ip)
	var dm entity.Dimensions
	json.Unmarshal(model.Dimensions, &dm)
	var wg entity.Weight
	json.Unmarshal(model.Weight, &wg)
	var vl entity.Volume
	json.Unmarshal(model.Volume, &vl)

	var deletedAt *time.Time
	if model.DeletedAt.Valid {
		deletedAt = &model.DeletedAt.Time
	}

	p, _ := entity.ReconstituteWithVersion(
		model.ID, model.CompanyID, model.CategoryID, model.BrandID, model.BaseUOMID,
		entity.SKU(model.SKU), entity.ProductName(model.Name), model.Description,
		tc, ip, dm, wg, vl, entity.Status(model.Status), model.CreatedAt, model.UpdatedAt, deletedAt, model.Version,
	)

	for _, b := range model.Barcodes {
		p.ReconstituteBarcode(b.ID, b.Code, b.Type, b.IsPrimary)
	}
	for _, u := range model.AlternateUOMs {
		p.ReconstituteAlternateUOM(u.ID, u.UOMID, u.Factor)
	}

	return p
}

func toDomainSlice(models []productModel) []*entity.Product {
	items := make([]*entity.Product, len(models))
	for i := range models {
		items[i] = toDomain(&models[i])
	}
	return items
}
