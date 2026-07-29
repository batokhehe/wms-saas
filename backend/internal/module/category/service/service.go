package service

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/internal/module/category/dto"
	"github.com/batokhehe/wms-saas/backend/internal/module/category/entity"
	"github.com/batokhehe/wms-saas/backend/internal/module/category/mapper"
	"github.com/batokhehe/wms-saas/backend/internal/module/category/repository"
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

func (s *Service) Create(ctx context.Context, req dto.CreateCategoryRequest) (dto.CategoryResponse, error) {
	companyID, _, err := s.actor(ctx)
	if err != nil {
		return dto.CategoryResponse{}, err
	}

	var response dto.CategoryResponse
	err = s.tx.RunInTransaction(ctx, func(ctx context.Context) error {
		taken, err := s.repo.ExistsByCode(ctx, companyID, req.Code)
		if err != nil {
			return err
		}
		if taken {
			return apperror.Conflict("code exists")
		}
		c, err := entity.New(s.ids.NewID(), companyID, req.Code, req.Name, req.Description, s.clock.Now())
		if err != nil {
			return err
		}
		if err := s.repo.Save(ctx, c); err != nil {
			return err
		}
		response = mapper.ToResponse(c)
		return nil
	})
	return response, err
}

func (s *Service) Update(ctx context.Context, id uuid.UUID, req dto.UpdateCategoryRequest) (dto.CategoryResponse, error) {
	return s.mutate(ctx, id, func(c *entity.Category, now time.Time) error {
		return c.UpdateDetails(req.Name, req.Description, now)
	})
}

func (s *Service) Activate(ctx context.Context, id uuid.UUID) (dto.CategoryResponse, error) {
	return s.mutate(ctx, id, func(c *entity.Category, now time.Time) error {
		c.Activate(now)
		return nil
	})
}

func (s *Service) Deactivate(ctx context.Context, id uuid.UUID) (dto.CategoryResponse, error) {
	return s.mutate(ctx, id, func(c *entity.Category, now time.Time) error {
		c.Deactivate(now)
		return nil
	})
}

func (s *Service) Archive(ctx context.Context, id uuid.UUID) error {
	companyID, _, err := s.actor(ctx)
	if err != nil {
		return err
	}
	return s.tx.RunInTransaction(ctx, func(ctx context.Context) error {
		c, err := s.repo.FindByID(ctx, id, companyID)
		if err != nil {
			return err
		}
		if err := c.Archive(s.clock.Now()); err != nil {
			return err
		}
		return s.repo.Update(ctx, c)
	})
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (dto.CategoryResponse, error) {
	companyID, _, err := s.actor(ctx)
	if err != nil {
		return dto.CategoryResponse{}, err
	}
	c, err := s.repo.FindByID(ctx, id, companyID)
	if err != nil {
		return dto.CategoryResponse{}, err
	}
	return mapper.ToResponse(c), nil
}

func (s *Service) List(ctx context.Context, filter repository.ListFilter) (pagination.Page[dto.CategoryResponse], error) {
	companyID, _, err := s.actor(ctx)
	if err != nil {
		return pagination.Page[dto.CategoryResponse]{}, err
	}
	page, err := s.repo.List(ctx, companyID, filter)
	if err != nil {
		return pagination.Page[dto.CategoryResponse]{}, err
	}
	return mapper.ToPage(page), nil
}

func (s *Service) mutate(ctx context.Context, id uuid.UUID, apply func(*entity.Category, time.Time) error) (dto.CategoryResponse, error) {
	companyID, _, err := s.actor(ctx)
	if err != nil {
		return dto.CategoryResponse{}, err
	}
	var response dto.CategoryResponse
	err = s.tx.RunInTransaction(ctx, func(ctx context.Context) error {
		c, err := s.repo.FindByID(ctx, id, companyID)
		if err != nil {
			return err
		}
		if err := apply(c, s.clock.Now()); err != nil {
			return err
		}
		if err := s.repo.Update(ctx, c); err != nil {
			return err
		}
		response = mapper.ToResponse(c)
		return nil
	})
	if err != nil {
		return dto.CategoryResponse{}, err
	}
	return response, nil
}
