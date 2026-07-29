package service

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/internal/module/product/dto"
	"github.com/batokhehe/wms-saas/backend/internal/module/product/entity"
	"github.com/batokhehe/wms-saas/backend/internal/module/product/mapper"
	"github.com/batokhehe/wms-saas/backend/internal/module/product/repository"
	"github.com/batokhehe/wms-saas/backend/internal/shared/appcontext"
	"github.com/batokhehe/wms-saas/backend/internal/shared/pagination"
	"github.com/batokhehe/wms-saas/backend/internal/shared/port"
	"github.com/batokhehe/wms-saas/backend/internal/shared/transaction"
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

type Service struct {
	repo              repository.Repository
	clock             port.Clock
	ids               port.IDGenerator
	tx                transaction.Manager
	categoryVerifier  CategoryVerifier
	brandVerifier     BrandVerifier
	uomVerifier       UOMVerifier
	inventoryVerifier InventoryVerifier
	stockVerifier     StockVerifier
}

func New(repo repository.Repository, clock port.Clock, ids port.IDGenerator, tx transaction.Manager, cv CategoryVerifier, bv BrandVerifier, uv UOMVerifier, iv InventoryVerifier, sv StockVerifier) *Service {
	return &Service{repo: repo, clock: clock, ids: ids, tx: tx, categoryVerifier: cv, brandVerifier: bv, uomVerifier: uv, inventoryVerifier: iv, stockVerifier: sv}
}

func (s *Service) actor(ctx context.Context) (uuid.UUID, uuid.UUID, error) {
	rc := appcontext.From(ctx)
	u, err := rc.RequireUser()
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	t, err := rc.RequireTenant()
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	return t, u, nil
}

func (s *Service) CreateProduct(ctx context.Context, req dto.CreateProductRequest) (dto.ProductResponse, error) {
	companyID, _, err := s.actor(ctx)
	if err != nil {
		return dto.ProductResponse{}, err
	}

	var response dto.ProductResponse
	err = s.tx.RunInTransaction(ctx, func(ctx context.Context) error {
		exists, err := s.repo.ExistsBySKU(ctx, companyID, entity.SKU(req.SKU))
		if err != nil {
			return err
		}
		if exists {
			return apperror.Conflict("SKU exists")
		}

		if ok, err := s.categoryVerifier.Exists(ctx, req.CategoryID); err != nil || !ok {
			return apperror.Validation("invalid category")
		}
		if ok, err := s.brandVerifier.Exists(ctx, req.BrandID); err != nil || !ok {
			return apperror.Validation("invalid brand")
		}
		if ok, err := s.uomVerifier.Exists(ctx, req.BaseUOMID); err != nil || !ok {
			return apperror.Validation("invalid UOM")
		}

		sku, _ := entity.NewSKU(req.SKU)
		name, _ := entity.NewProductName(req.Name)
		p, err := entity.New(s.ids.NewID(), companyID, req.CategoryID, req.BrandID, req.BaseUOMID, sku, name, req.Description,
			entity.TrackingConfig(req.TrackingConfig), entity.InventoryPolicy(req.InventoryPolicy), entity.Dimensions{}, entity.Weight{}, entity.Volume{}, s.clock.Now())
		if err != nil {
			return err
		}
		if err := s.repo.Save(ctx, p); err != nil {
			return err
		}
		response = mapper.ToResponse(p)
		return nil
	})
	return response, err
}

func (s *Service) UpdateProduct(ctx context.Context, id uuid.UUID, req dto.UpdateProductRequest) (dto.ProductResponse, error) {
	return s.mutate(ctx, id, func(p *entity.Product, now time.Time) error {
		name, _ := entity.NewProductName(req.Name)
		return p.UpdateDetails(name, req.Description, now)
	})
}

func (s *Service) ActivateProduct(ctx context.Context, id uuid.UUID) (dto.ProductResponse, error) {
	return s.mutate(ctx, id, func(p *entity.Product, now time.Time) error {
		p.Activate(now)
		return nil
	})
}

func (s *Service) DeactivateProduct(ctx context.Context, id uuid.UUID) (dto.ProductResponse, error) {
	return s.mutate(ctx, id, func(p *entity.Product, now time.Time) error {
		p.Deactivate(now)
		return nil
	})
}

func (s *Service) ArchiveProduct(ctx context.Context, id uuid.UUID) error {
	companyID, _, err := s.actor(ctx)
	if err != nil {
		return err
	}
	return s.tx.RunInTransaction(ctx, func(ctx context.Context) error {
		p, err := s.repo.FindByID(ctx, id, companyID)
		if err != nil {
			return err
		}
		if err := p.Archive(s.clock.Now(), s.inventoryVerifier); err != nil {
			return err
		}
		return s.repo.Update(ctx, p)
	})
}

func (s *Service) GetProductByID(ctx context.Context, id uuid.UUID) (dto.ProductResponse, error) {
	companyID, _, err := s.actor(ctx)
	if err != nil {
		return dto.ProductResponse{}, err
	}
	p, err := s.repo.FindByID(ctx, id, companyID)
	if err != nil {
		return dto.ProductResponse{}, err
	}
	return mapper.ToResponse(p), nil
}

func (s *Service) ListProducts(ctx context.Context, query dto.ListProductQuery) (pagination.Page[dto.ProductResponse], error) {
	companyID, _, err := s.actor(ctx)
	if err != nil {
		return pagination.Page[dto.ProductResponse]{}, err
	}

	if err := query.Request.Apply(dto.SortOptions()); err != nil {
		return pagination.Page[dto.ProductResponse]{}, err
	}

	page, err := s.repo.List(ctx, companyID, repository.ListFilter{Paging: query.Request, Status: query.Status})
	if err != nil {
		return pagination.Page[dto.ProductResponse]{}, err
	}
	return mapper.ToPage(page), nil
}

func (s *Service) mutate(ctx context.Context, id uuid.UUID, apply func(*entity.Product, time.Time) error) (dto.ProductResponse, error) {
	companyID, _, err := s.actor(ctx)
	if err != nil {
		return dto.ProductResponse{}, err
	}
	var response dto.ProductResponse
	err = s.tx.RunInTransaction(ctx, func(ctx context.Context) error {
		p, err := s.repo.FindByID(ctx, id, companyID)
		if err != nil {
			return err
		}
		if err := apply(p, s.clock.Now()); err != nil {
			return err
		}
		if err := s.repo.Update(ctx, p); err != nil {
			return err
		}
		response = mapper.ToResponse(p)
		return nil
	})
	if err != nil {
		return dto.ProductResponse{}, err
	}
	return response, nil
}
