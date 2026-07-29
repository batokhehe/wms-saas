package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/batokhehe/wms-saas/backend/internal/module/category/dto"
	"github.com/batokhehe/wms-saas/backend/internal/module/category/entity"
	"github.com/batokhehe/wms-saas/backend/internal/module/category/repository"
	"github.com/batokhehe/wms-saas/backend/internal/module/category/service"
	"github.com/batokhehe/wms-saas/backend/internal/shared/appcontext"
	"github.com/batokhehe/wms-saas/backend/internal/shared/pagination"
	sharedrepo "github.com/batokhehe/wms-saas/backend/internal/shared/repository"
	"github.com/batokhehe/wms-saas/backend/internal/shared/transaction"
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time { return c.now }

type fakeIDGenerator struct{ id uuid.UUID }

func (g *fakeIDGenerator) NewID() uuid.UUID { return g.id }

func ctxWithActor(tID, uID uuid.UUID) context.Context {
	return appcontext.Into(context.Background(), appcontext.New(uID, tID, nil))
}

func TestService(t *testing.T) {
	repo := newFakeRepo()
	svc := service.New(repo, &fakeClock{time.Now()}, &fakeIDGenerator{uuid.New()}, &transaction.NoOpManager{})
	tID, uID := uuid.New(), uuid.New()
	ctx := ctxWithActor(tID, uID)

	t.Run("Create", func(t *testing.T) {
		req := dto.CreateCategoryRequest{Code: "CAT1", Name: "Category 1"}
		res, err := svc.Create(ctx, req)
		require.NoError(t, err)
		assert.Equal(t, "CAT1", res.Code)
	})

	t.Run("DuplicateCode", func(t *testing.T) {
		_, err := svc.Create(ctx, dto.CreateCategoryRequest{Code: "CAT1", Name: "Category 2"})
		assert.ErrorIs(t, err, apperror.ErrConflict)
	})

	t.Run("Archive", func(t *testing.T) {
		c, _ := entity.New(uuid.New(), tID, "CAT2", "Name", "", time.Now())
		repo.Save(ctx, c)
		err := svc.Archive(ctx, c.ID())
		require.NoError(t, err)
		updated, _ := repo.FindByID(ctx, c.ID(), tID)
		assert.True(t, updated.IsArchived())
	})

	t.Run("Update", func(t *testing.T) {
		c, _ := entity.New(uuid.New(), tID, "CAT3", "Name", "", time.Now())
		repo.Save(ctx, c)
		res, err := svc.Update(ctx, c.ID(), dto.UpdateCategoryRequest{Name: "New Name"})
		require.NoError(t, err)
		assert.Equal(t, "New Name", res.Name)
	})

	t.Run("ActivateDeactivate", func(t *testing.T) {
		c, _ := entity.New(uuid.New(), tID, "CAT4", "Name", "", time.Now())
		repo.Save(ctx, c)
		res, err := svc.Deactivate(ctx, c.ID())
		require.NoError(t, err)
		assert.Equal(t, "INACTIVE", res.Status)
		res, err = svc.Activate(ctx, c.ID())
		require.NoError(t, err)
		assert.Equal(t, "ACTIVE", res.Status)
	})

	t.Run("GetByID", func(t *testing.T) {
		c, _ := entity.New(uuid.New(), tID, "CAT5", "Name", "", time.Now())
		repo.Save(ctx, c)
		res, err := svc.Get(ctx, c.ID())
		require.NoError(t, err)
		assert.Equal(t, c.ID(), res.ID)
	})

	t.Run("List", func(t *testing.T) {
		page, err := svc.List(ctx, repository.ListFilter{})
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(page.Items), 1)
	})

	t.Run("OptimisticLocking", func(t *testing.T) {
		c, _ := entity.New(uuid.New(), tID, "CAT6", "Name", "", time.Now())
		repo.Save(ctx, c)
		repo.fail("Update", sharedrepo.ErrConcurrentModification)
		_, err := svc.Activate(ctx, c.ID())
		assert.ErrorIs(t, err, sharedrepo.ErrConcurrentModification)
	})

	t.Run("RepositoryError", func(t *testing.T) {
		repo.fail("FindByID", apperror.Internal("db failure"))
		_, err := svc.Get(ctx, uuid.New())
		assert.Error(t, err)
	})
}
