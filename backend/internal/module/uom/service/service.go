package service

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/internal/module/uom/dto"
	"github.com/batokhehe/wms-saas/backend/internal/module/uom/entity"
	"github.com/batokhehe/wms-saas/backend/internal/module/uom/mapper"
	"github.com/batokhehe/wms-saas/backend/internal/module/uom/repository"
	"github.com/batokhehe/wms-saas/backend/internal/shared/pagination"
	"github.com/batokhehe/wms-saas/backend/internal/shared/port"
	sharedrepo "github.com/batokhehe/wms-saas/backend/internal/shared/repository"
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
	return &Service{
		repo:  repo,
		clock: clock,
		ids:   ids,
		tx:    tx,
	}
}

func (s *Service) CreateUOM(ctx context.Context, req dto.CreateUOMRequest) (dto.UOMResponse, error) {
	var response dto.UOMResponse
	err := s.tx.RunInTransaction(ctx, func(ctx context.Context) error {
		if err := s.assertCodeAvailable(ctx, req.Code); err != nil {
			return err
		}

		uom, err := entity.New(s.ids.NewID(), req.Code, req.Name, req.Description, s.clock.Now())
		if err != nil {
			return err
		}

		if err := s.repo.Save(ctx, uom); err != nil {
			return err
		}

		response = mapper.ToResponse(uom)
		return nil
	})
	if err != nil {
		return dto.UOMResponse{}, err
	}
	return response, nil
}

func (s *Service) UpdateUOM(ctx context.Context, id uuid.UUID, req dto.UpdateUOMRequest) (dto.UOMResponse, error) {
	return s.mutate(ctx, id, "UpdateUOM",
		func(u *entity.UOM, now time.Time) error {
			return u.UpdateDetails(req.Name, req.Description, now)
		})
}

func (s *Service) ActivateUOM(ctx context.Context, id uuid.UUID) (dto.UOMResponse, error) {
	return s.mutate(ctx, id, "ActivateUOM",
		func(u *entity.UOM, now time.Time) error {
			u.Activate(now)
			return nil
		})
}

func (s *Service) DeactivateUOM(ctx context.Context, id uuid.UUID) (dto.UOMResponse, error) {
	return s.mutate(ctx, id, "DeactivateUOM",
		func(u *entity.UOM, now time.Time) error {
			u.Deactivate(now)
			return nil
		})
}

func (s *Service) GetUOMByID(ctx context.Context, id uuid.UUID) (dto.UOMResponse, error) {
	uom, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return dto.UOMResponse{}, err
	}
	return mapper.ToResponse(uom), nil
}

func (s *Service) ListUOM(ctx context.Context, filter repository.ListFilter) (pagination.Page[dto.UOMResponse], error) {
	page, err := s.repo.List(ctx, filter)
	if err != nil {
		return pagination.Page[dto.UOMResponse]{}, err
	}
	return mapper.ToPage(page), nil
}

func (s *Service) mutate(
	ctx context.Context,
	id uuid.UUID,
	op string,
	apply func(*entity.UOM, time.Time) error,
) (dto.UOMResponse, error) {
	var response dto.UOMResponse
	err := s.tx.RunInTransaction(ctx, func(ctx context.Context) error {
		uom, err := s.repo.FindByID(ctx, id)
		if err != nil {
			return err
		}

		if err := apply(uom, s.clock.Now()); err != nil {
			return err
		}

		if err := s.repo.Update(ctx, uom); err != nil {
			return err
		}

		response = mapper.ToResponse(uom)
		return nil
	})
	if err != nil {
		return dto.UOMResponse{}, s.concurrentModification(ctx, id, err)
	}

	return response, nil
}

func (s *Service) concurrentModification(ctx context.Context, id uuid.UUID, err error) error {
	if !errors.Is(err, sharedrepo.ErrConcurrentModification) {
		return err
	}

	current, readErr := s.repo.FindByID(ctx, id)
	if readErr != nil {
		return err
	}
	return apperror.Conflict("The resource was changed by another request").
		WithOp("uom.service.concurrentModification").
		WithDetails(map[string]any{"entity_id": id, "current_version": current.Version()}).
		WithCause(sharedrepo.ErrConcurrentModification)
}

func (s *Service) assertCodeAvailable(ctx context.Context, code string) error {
	taken, err := s.repo.ExistsByCode(ctx, code)
	if err != nil {
		return err
	}
	if taken {
		return apperror.Conflict("A UOM with this code already exists").
			WithOp("uom.service.assertCodeAvailable")
	}
	return nil
}
