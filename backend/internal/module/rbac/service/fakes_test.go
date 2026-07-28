package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/batokhehe/wms-saas/backend/internal/module/rbac/dto"
	"github.com/batokhehe/wms-saas/backend/internal/module/rbac/entity"
	"github.com/batokhehe/wms-saas/backend/internal/shared/pagination"
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// In-memory doubles for the three repositories, the transaction manager and the
// event publisher.
//
// The role fake reimplements tenant filtering in Go. That is deliberate: a fake
// that ignored the companyID argument would make every cross-company isolation
// test pass while the real query leaked, so it enforces the same rule the
// scopes do. The SQL itself is verified separately by the integration suite.

func fixedNow() time.Time { return time.Date(2026, 7, 24, 10, 0, 0, 0, time.UTC) }

// ---------- roles ----------

type fakeRoleRepo struct {
	mu     sync.Mutex
	byID   map[uuid.UUID]*entity.Role
	seq    int
	failOn map[string]error
}

func newFakeRoleRepo() *fakeRoleRepo {
	return &fakeRoleRepo{byID: map[uuid.UUID]*entity.Role{}, failOn: map[string]error{}}
}

func (r *fakeRoleRepo) fail(method string, err error) { r.failOn[method] = err }

func (r *fakeRoleRepo) Create(_ context.Context, role *entity.Role) error {
	if err := r.failOn["Create"]; err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Mirrors ux_roles_company_name.
	for _, existing := range r.byID {
		if existing.CompanyID == role.CompanyID &&
			entity.NormalizeRoleName(existing.Name) == entity.NormalizeRoleName(role.Name) {
			return apperror.Conflict("duplicate role name").WithOp("fake.Create")
		}
	}

	if role.ID == uuid.Nil {
		r.seq++
		role.ID = uuid.MustParse(seqUUID(r.seq))
	}
	role.Name = entity.NormalizeRoleName(role.Name)
	role.CreatedAt = fixedNow()
	role.UpdatedAt = role.CreatedAt

	stored := *role
	r.byID[role.ID] = &stored
	return nil
}

func (r *fakeRoleRepo) Update(_ context.Context, role *entity.Role) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.byID[role.ID]; !ok {
		return apperror.NotFound("role not found").WithOp("fake.Update")
	}
	stored := *role
	r.byID[role.ID] = &stored
	return nil
}

func (r *fakeRoleRepo) FindByID(
	_ context.Context, roleID, companyID uuid.UUID,
) (*entity.Role, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	role, ok := r.byID[roleID]
	// The companyID check IS the tenant filter under test.
	if !ok || role.CompanyID != companyID {
		return nil, apperror.NotFound("role not found").WithOp("fake.FindByID")
	}
	clone := *role
	return &clone, nil
}

func (r *fakeRoleRepo) FindByName(
	_ context.Context, companyID uuid.UUID, name string,
) (*entity.Role, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	normalized := entity.NormalizeRoleName(name)
	for _, role := range r.byID {
		if role.CompanyID == companyID && entity.NormalizeRoleName(role.Name) == normalized {
			clone := *role
			return &clone, nil
		}
	}
	return nil, apperror.NotFound("role not found").WithOp("fake.FindByName")
}

func (r *fakeRoleRepo) List(
	_ context.Context, companyID uuid.UUID, query dto.ListRolesQuery,
) (pagination.Page[entity.Role], error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	matched := make([]entity.Role, 0)
	for _, role := range r.byID {
		if role.CompanyID != companyID {
			continue
		}
		if query.IsSystem != nil && role.IsSystem != *query.IsSystem {
			continue
		}
		matched = append(matched, *role)
	}

	return pagination.NewPage(matched, query.Request, int64(len(matched))), nil
}

func (r *fakeRoleRepo) ListAll(
	_ context.Context, companyID uuid.UUID,
) ([]entity.Role, error) {
	if err := r.failOn["ListAll"]; err != nil {
		return nil, err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	matched := make([]entity.Role, 0)
	for _, role := range r.byID {
		if role.CompanyID == companyID {
			matched = append(matched, *role)
		}
	}
	return matched, nil
}

func (r *fakeRoleRepo) Delete(_ context.Context, roleID, companyID uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	role, ok := r.byID[roleID]
	if !ok || role.CompanyID != companyID {
		return apperror.NotFound("role not found").WithOp("fake.Delete")
	}
	delete(r.byID, roleID)
	return nil
}

func (r *fakeRoleRepo) ExistsByName(
	_ context.Context, companyID uuid.UUID, name string,
) (bool, error) {
	_, err := r.FindByName(context.Background(), companyID, name)
	if err != nil {
		return false, nil
	}
	return true, nil
}

func (r *fakeRoleRepo) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.byID)
}

// ---------- permissions ----------

type fakePermissionRepo struct {
	mu     sync.Mutex
	byCode map[entity.Code]*entity.Permission
	grants *fakeGrantRepo
}

// newFakePermissionRepo seeds the catalogue exactly as the migration does, so a
// drift between entity.PermissionCatalogue and the seed would fail here too.
func newFakePermissionRepo(grants *fakeGrantRepo) *fakePermissionRepo {
	repo := &fakePermissionRepo{
		byCode: map[entity.Code]*entity.Permission{},
		grants: grants,
	}

	for i, code := range entity.PermissionCatalogue() {
		permission := &entity.Permission{
			Code:   code,
			Name:   string(code),
			Module: moduleOf(code),
		}
		permission.SetID(uuid.MustParse(seqUUID(9000 + i)))
		repo.byCode[code] = permission
	}

	return repo
}

func moduleOf(code entity.Code) string {
	for i, r := range code {
		if r == '.' {
			return string(code)[:i]
		}
	}
	return string(code)
}

func (r *fakePermissionRepo) List(
	_ context.Context, module string,
) ([]entity.Permission, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	result := make([]entity.Permission, 0, len(r.byCode))
	for _, permission := range r.byCode {
		if module != "" && permission.Module != module {
			continue
		}
		result = append(result, *permission)
	}
	return result, nil
}

func (r *fakePermissionRepo) FindByCodes(
	_ context.Context, codes []entity.Code,
) ([]entity.Permission, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	result := make([]entity.Permission, 0, len(codes))
	for _, code := range codes {
		if permission, ok := r.byCode[code]; ok {
			result = append(result, *permission)
		}
	}
	return result, nil
}

func (r *fakePermissionRepo) CodesByRole(
	_ context.Context, roleIDs []uuid.UUID,
) (map[uuid.UUID][]entity.Code, error) {
	byID := map[uuid.UUID]entity.Code{}
	r.mu.Lock()
	for code, permission := range r.byCode {
		byID[permission.ID] = code
	}
	r.mu.Unlock()

	result := make(map[uuid.UUID][]entity.Code, len(roleIDs))
	for _, roleID := range roleIDs {
		for _, permissionID := range r.grants.liveFor(roleID) {
			if code, ok := byID[permissionID]; ok {
				result[roleID] = append(result[roleID], code)
			}
		}
	}
	return result, nil
}

// idFor exposes a code's id, for tests that assert on grants directly.
func (r *fakePermissionRepo) idFor(code entity.Code) uuid.UUID {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.byCode[code].ID
}

// ---------- grants ----------

type grantRow struct {
	roleID       uuid.UUID
	permissionID uuid.UUID
	deleted      bool
}

type fakeGrantRepo struct {
	mu   sync.Mutex
	rows []*grantRow
}

func newFakeGrantRepo() *fakeGrantRepo { return &fakeGrantRepo{} }

func (r *fakeGrantRepo) Grant(
	_ context.Context, roleID uuid.UUID, permissionIDs []uuid.UUID,
) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var affected int64

	for _, permissionID := range permissionIDs {
		found := false
		for _, row := range r.rows {
			if row.roleID == roleID && row.permissionID == permissionID {
				found = true
				// Revive a soft-deleted grant rather than adding a second row,
				// mirroring the real repository.
				if row.deleted {
					row.deleted = false
					affected++
				}
			}
		}
		if !found {
			r.rows = append(r.rows, &grantRow{roleID: roleID, permissionID: permissionID})
			affected++
		}
	}

	return affected, nil
}

func (r *fakeGrantRepo) Revoke(
	_ context.Context, roleID uuid.UUID, permissionIDs []uuid.UUID, _ time.Time,
) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	targets := make(map[uuid.UUID]struct{}, len(permissionIDs))
	for _, id := range permissionIDs {
		targets[id] = struct{}{}
	}

	var affected int64
	for _, row := range r.rows {
		if row.roleID != roleID || row.deleted {
			continue
		}
		if _, ok := targets[row.permissionID]; ok {
			row.deleted = true
			affected++
		}
	}
	return affected, nil
}

func (r *fakeGrantRepo) PermissionIDs(
	_ context.Context, roleID uuid.UUID,
) ([]uuid.UUID, error) {
	return r.liveFor(roleID), nil
}

func (r *fakeGrantRepo) liveFor(roleID uuid.UUID) []uuid.UUID {
	r.mu.Lock()
	defer r.mu.Unlock()

	ids := make([]uuid.UUID, 0)
	for _, row := range r.rows {
		if row.roleID == roleID && !row.deleted {
			ids = append(ids, row.permissionID)
		}
	}
	return ids
}

// ---------- transaction manager ----------

// fakeTxManager simulates real transaction semantics, including ROLLBACK.
//
// It snapshots the fakes before running fn and restores them on error. Without
// this a failed flow would commit partial work in the fake, and a whole class
// of bug becomes invisible to unit tests — the auth sprint hit exactly that.
type fakeTxManager struct {
	roles  *fakeRoleRepo
	grants *fakeGrantRepo

	calls     int
	rollbacks int
	depth     int
}

func (m *fakeTxManager) RunInTransaction(ctx context.Context, fn func(context.Context) error) error {
	m.calls++

	if m.depth > 0 {
		return fn(ctx)
	}

	snapRoles, snapGrants := m.snapshot()

	m.depth++
	err := fn(ctx)
	m.depth--

	if err != nil {
		m.rollbacks++
		m.restore(snapRoles, snapGrants)
		return err
	}
	return nil
}

func (m *fakeTxManager) snapshot() (map[uuid.UUID]entity.Role, []grantRow) {
	roles := map[uuid.UUID]entity.Role{}
	if m.roles != nil {
		m.roles.mu.Lock()
		for id, role := range m.roles.byID {
			roles[id] = *role
		}
		m.roles.mu.Unlock()
	}

	var grants []grantRow
	if m.grants != nil {
		m.grants.mu.Lock()
		for _, row := range m.grants.rows {
			grants = append(grants, *row)
		}
		m.grants.mu.Unlock()
	}

	return roles, grants
}

func (m *fakeTxManager) restore(roles map[uuid.UUID]entity.Role, grants []grantRow) {
	if m.roles != nil {
		m.roles.mu.Lock()
		m.roles.byID = map[uuid.UUID]*entity.Role{}
		for id, role := range roles {
			clone := role
			m.roles.byID[id] = &clone
		}
		m.roles.mu.Unlock()
	}

	if m.grants != nil {
		m.grants.mu.Lock()
		m.grants.rows = nil
		for _, row := range grants {
			clone := row
			m.grants.rows = append(m.grants.rows, &clone)
		}
		m.grants.mu.Unlock()
	}
}

// ---------- event publisher ----------

type fakeEventPublisher struct {
	mu     sync.Mutex
	events []entity.Event
}

func (p *fakeEventPublisher) Publish(_ context.Context, event entity.Event) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, event)
}

func (p *fakeEventPublisher) names() []entity.EventName {
	p.mu.Lock()
	defer p.mu.Unlock()

	names := make([]entity.EventName, 0, len(p.events))
	for _, e := range p.events {
		names = append(names, e.Name)
	}
	return names
}

func (p *fakeEventPublisher) has(name entity.EventName) bool {
	for _, got := range p.names() {
		if got == name {
			return true
		}
	}
	return false
}

func (p *fakeEventPublisher) find(name entity.EventName) (entity.Event, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, e := range p.events {
		if e.Name == name {
			return e, true
		}
	}
	return entity.Event{}, false
}

func (p *fakeEventPublisher) reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = nil
}

// ---------- helpers ----------

func seqUUID(n int) string {
	return fmt.Sprintf("00000000-0000-4000-8000-%012d", n)
}

func zapNop() *zap.Logger { return zap.NewNop() }
