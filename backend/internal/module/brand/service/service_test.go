package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/batokhehe/wms-saas/backend/internal/module/brand/dto"
	"github.com/batokhehe/wms-saas/backend/internal/module/brand/entity"
	"github.com/batokhehe/wms-saas/backend/internal/module/brand/repository"
	"github.com/batokhehe/wms-saas/backend/internal/module/brand/service"
	"github.com/batokhehe/wms-saas/backend/internal/module/brand/service/testdata/shared"
	"github.com/batokhehe/wms-saas/backend/internal/shared/appcontext"
	sharedrepo "github.com/batokhehe/wms-saas/backend/internal/shared/repository"
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
	"go.uber.org/zap"
)

type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time { return c.now }

type fakeIDGenerator struct{ id uuid.UUID }

func (g *fakeIDGenerator) NewID() uuid.UUID { return g.id }

func ctxWithActor(tID, uID uuid.UUID) context.Context {
	rc := appcontext.New("test-req-id", zap.NewNop())
	rc.WithCompany(tID, uuid.New(), "admin")
	rc.WithTenant(&tID, &uID, "admin")
	return appcontext.Into(context.Background(), rc)
}

func TestService(t *testing.T) {
	repo := newFakeRepo()
	svc := service.New(repo, &fakeClock{time.Now()}, &fakeIDGenerator{uuid.New()}, &testutils.NoOpManager{})
	tID, uID := uuid.New(), uuid.New()
	ctx := ctxWithActor(tID, uID)

	t.Run("Create", func(t *testing.T) {
		req := dto.CreateBrandRequest{Code: "B1", Name: "Brand 1"}
		res, err := svc.Create(ctx, req)
		require.NoError(t, err)
		assert.Equal(t, "B1", res.Code)
	})

	t.Run("DuplicateCode", func(t *testing.T) {
		_, err := svc.Create(ctx, dto.CreateBrandRequest{Code: "B1", Name: "Brand 2"})
		assert.ErrorIs(t, err, apperror.ErrConflict)
	})

	t.Run("Archive", func(t *testing.T) {
		b, _ := entity.New(uuid.New(), tID, "B2", "Name", "", time.Now())
		repo.Save(ctx, b)
		err := svc.Archive(ctx, b.ID())
		require.NoError(t, err)
		updated, _ := repo.FindByID(ctx, b.ID(), tID)
		assert.True(t, updated.IsArchived())
	})

	t.Run("Update", func(t *testing.T) {
		b, _ := entity.New(uuid.New(), tID, "B3", "Name", "", time.Now())
		repo.Save(ctx, b)
		res, err := svc.Update(ctx, b.ID(), dto.UpdateBrandRequest{Name: "New Name"})
		require.NoError(t, err)
		assert.Equal(t, "New Name", res.Name)
	})

	t.Run("ActivateDeactivate", func(t *testing.T) {
		b, _ := entity.New(uuid.New(), tID, "B4", "Name", "", time.Now())
		repo.Save(ctx, b)
		res, err := svc.Deactivate(ctx, b.ID())
		require.NoError(t, err)
		assert.Equal(t, "INACTIVE", res.Status)
		res, err = svc.Activate(ctx, b.ID())
		require.NoError(t, err)
		assert.Equal(t, "ACTIVE", res.Status)
	})

	t.Run("GetByID", func(t *testing.T) {
		b, _ := entity.New(uuid.New(), tID, "B5", "Name", "", time.Now())
		repo.Save(ctx, b)
		res, err := svc.Get(ctx, b.ID())
		require.NoError(t, err)
		assert.Equal(t, b.ID(), res.ID)
	})

	t.Run("List", func(t *testing.T) {
		page, err := svc.List(ctx, repository.ListFilter{})
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(page.Items), 1)
	})

	t.Run("OptimisticLocking", func(t *testing.T) {
		b, _ := entity.New(uuid.New(), tID, "B6", "Name", "", time.Now())
		repo.Save(ctx, b)
		repo.fail("Update", sharedrepo.ErrConcurrentModification)
		_, err := svc.Activate(ctx, b.ID())
		assert.ErrorIs(t, err, sharedrepo.ErrConcurrentModification)
	})

	t.Run("RepositoryError", func(t *testing.T) {
		repo.fail("FindByID", apperror.Internal("db failure"))
		_, err := svc.Get(ctx, uuid.New())
		assert.Error(t, err)
	})
}
