package service

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/internal/module/brand/dto"
	"github.com/batokhehe/wms-saas/backend/internal/module/brand/entity"
	"github.com/batokhehe/wms-saas/backend/internal/module/brand/mapper"
	"github.com/batokhehe/wms-saas/backend/internal/module/brand/repository"
	"github.com/batokhehe/wms-saas/backend/internal/shared/appcontext"
	"github.com/batokhehe/wms-saas/backend/internal/shared/pagination"
	"github.com/batokhehe/wms-saas/backend/internal/shared/port"
	"github.com/batokhehe/wms-saas/backend/internal/shared/transaction"
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

type Service struct {
	repo  repository.Repository
	clock port.Clock
	ids   port.IDGenerator
	tx    transaction.Manager
}

func New(repo repository.Repository, clock port.Clock, ids port.IDGenerator, tx transaction.Manager) *Service {
	return &Service{repo: repo, clock: clock, ids: ids, tx: tx}
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

func (s *Service) Create(ctx context.Context, req dto.CreateBrandRequest) (dto.BrandResponse, error) {
	companyID, _, err := s.actor(ctx)
	if err != nil {
		return dto.BrandResponse{}, err
	}

	var response dto.BrandResponse
	err = s.tx.RunInTransaction(ctx, func(ctx context.Context) error {
		taken, err := s.repo.ExistsByCode(ctx, companyID, req.Code)
		if err != nil {
			return err
		}
		if taken {
			return apperror.Conflict("code exists")
		}
		b, err := entity.New(s.ids.NewID(), companyID, req.Code, req.Name, req.Description, s.clock.Now())
		if err != nil {
			return err
		}
		if err := s.repo.Save(ctx, b); err != nil {
			return err
		}
		response = mapper.ToResponse(b)
		return nil
	})
	return response, err
}

func (s *Service) Update(ctx context.Context, id uuid.UUID, req dto.UpdateBrandRequest) (dto.BrandResponse, error) {
	return s.mutate(ctx, id, func(b *entity.Brand, now time.Time) error {
		return b.UpdateDetails(req.Name, req.Description, now)
	})
}

func (s *Service) Activate(ctx context.Context, id uuid.UUID) (dto.BrandResponse, error) {
	return s.mutate(ctx, id, func(b *entity.Brand, now time.Time) error {
		b.Activate(now)
		return nil
	})
}

func (s *Service) Deactivate(ctx context.Context, id uuid.UUID) (dto.BrandResponse, error) {
	return s.mutate(ctx, id, func(b *entity.Brand, now time.Time) error {
		b.Deactivate(now)
		return nil
	})
}

func (s *Service) Archive(ctx context.Context, id uuid.UUID) error {
	companyID, _, err := s.actor(ctx)
	if err != nil {
		return err
	}
	return s.tx.RunInTransaction(ctx, func(ctx context.Context) error {
		b, err := s.repo.FindByID(ctx, id, companyID)
		if err != nil {
			return err
		}
		if err := b.Archive(s.clock.Now()); err != nil {
			return err
		}
		return s.repo.Update(ctx, b)
	})
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (dto.BrandResponse, error) {
	companyID, _, err := s.actor(ctx)
	if err != nil {
		return dto.BrandResponse{}, err
	}
	b, err := s.repo.FindByID(ctx, id, companyID)
	if err != nil {
		return dto.BrandResponse{}, err
	}
	return mapper.ToResponse(b), nil
}

func (s *Service) List(ctx context.Context, filter repository.ListFilter) (pagination.Page[dto.BrandResponse], error) {
	companyID, _, err := s.actor(ctx)
	if err != nil {
		return pagination.Page[dto.BrandResponse]{}, err
	}
	page, err := s.repo.List(ctx, companyID, filter)
	if err != nil {
		return pagination.Page[dto.BrandResponse]{}, err
	}
	return mapper.ToPage(page), nil
}

func (s *Service) mutate(ctx context.Context, id uuid.UUID, apply func(*entity.Brand, time.Time) error) (dto.BrandResponse, error) {
	companyID, _, err := s.actor(ctx)
	if err != nil {
		return dto.BrandResponse{}, err
	}
	var response dto.BrandResponse
	err = s.tx.RunInTransaction(ctx, func(ctx context.Context) error {
		b, err := s.repo.FindByID(ctx, id, companyID)
		if err != nil {
			return err
		}
		if err := apply(b, s.clock.Now()); err != nil {
			return err
		}
		if err := s.repo.Update(ctx, b); err != nil {
			return err
		}
		response = mapper.ToResponse(b)
		return nil
	})
	if err != nil {
		return dto.BrandResponse{}, err
	}
	return response, nil
}
