package service

import (
	"context"
	"fmt"
	"sync"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/batokhehe/wms-saas/backend/internal/module/tenancy/dto"
	"github.com/batokhehe/wms-saas/backend/internal/module/tenancy/entity"
	"github.com/batokhehe/wms-saas/backend/internal/shared/pagination"
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// In-memory doubles for the repositories, the transaction manager and the event
// publisher. They let every tenancy flow — including cross-company isolation —
// be exercised with no database, which is the payoff of keeping the service
// free of gin and gorm imports.
//
// The fakes reimplement tenant filtering in Go rather than SQL. That is
// deliberate: a fake that ignored the companyID argument would make an
// isolation test pass while the real query leaked, so these enforce the same
// rule the scopes do.

// ---------- companies ----------

type fakeCompanyRepo struct {
	mu     sync.Mutex
	byID   map[uuid.UUID]*entity.Company
	seq    int
	failOn map[string]error

	// memberships is a back-reference so the fake can enforce the SAME
	// reachability rule the accessibleTo scope enforces in SQL.
	memberships *fakeMembershipRepo
}

func newFakeCompanyRepo(memberships *fakeMembershipRepo) *fakeCompanyRepo {
	return &fakeCompanyRepo{
		byID:        map[uuid.UUID]*entity.Company{},
		failOn:      map[string]error{},
		memberships: memberships,
	}
}

func (r *fakeCompanyRepo) fail(method string, err error) { r.failOn[method] = err }

func (r *fakeCompanyRepo) Create(_ context.Context, company *entity.Company) error {
	if err := r.failOn["Create"]; err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	for _, existing := range r.byID {
		if existing.Code == company.Code {
			return apperror.Conflict("duplicate code").WithOp("fake.Create")
		}
	}

	if company.ID == uuid.Nil {
		r.seq++
		company.ID = uuid.MustParse(seqUUID(r.seq))
	}
	company.CreatedAt = fixedNow()
	company.UpdatedAt = company.CreatedAt

	stored := *company
	r.byID[company.ID] = &stored
	return nil
}

func (r *fakeCompanyRepo) Update(_ context.Context, company *entity.Company) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.byID[company.ID]; !ok {
		return apperror.NotFound("company not found").WithOp("fake.Update")
	}
	stored := *company
	r.byID[company.ID] = &stored
	return nil
}

// canAccess mirrors the accessibleTo scope: an ACTIVE membership is required.
func (r *fakeCompanyRepo) canAccess(companyID, userID uuid.UUID) bool {
	if r.memberships == nil {
		return true
	}
	m := r.memberships.findActive(userID, companyID)
	return m != nil
}

func (r *fakeCompanyRepo) FindAccessible(
	_ context.Context, companyID, userID uuid.UUID,
) (*entity.Company, error) {
	if !r.canAccess(companyID, userID) {
		// NOT_FOUND, never FORBIDDEN: a company the caller cannot reach must
		// not be distinguishable from one that does not exist.
		return nil, apperror.NotFound("company not found").WithOp("fake.FindAccessible")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	company, ok := r.byID[companyID]
	if !ok {
		return nil, apperror.NotFound("company not found").WithOp("fake.FindAccessible")
	}
	clone := *company
	return &clone, nil
}

func (r *fakeCompanyRepo) ListAccessible(
	_ context.Context, userID uuid.UUID, query dto.ListCompaniesQuery,
) (pagination.Page[entity.Company], error) {
	r.mu.Lock()
	companies := make([]entity.Company, 0, len(r.byID))
	for _, company := range r.byID {
		companies = append(companies, *company)
	}
	r.mu.Unlock()

	matched := make([]entity.Company, 0, len(companies))
	for _, company := range companies {
		if !r.canAccess(company.ID, userID) {
			continue
		}
		if query.Status != "" && string(company.Status) != query.Status {
			continue
		}
		matched = append(matched, company)
	}

	return pagination.NewPage(matched, query.Request, int64(len(matched))), nil
}

func (r *fakeCompanyRepo) Delete(_ context.Context, companyID, userID uuid.UUID) error {
	if !r.canAccess(companyID, userID) {
		return apperror.NotFound("company not found").WithOp("fake.Delete")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.byID[companyID]; !ok {
		return apperror.NotFound("company not found").WithOp("fake.Delete")
	}
	delete(r.byID, companyID)
	return nil
}

func (r *fakeCompanyRepo) ExistsByCode(_ context.Context, code string) (bool, error) {
	if err := r.failOn["ExistsByCode"]; err != nil {
		return false, err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	normalized := entity.NormalizeCode(code)
	for _, company := range r.byID {
		if company.Code == normalized {
			return true, nil
		}
	}
	return false, nil
}

func (r *fakeCompanyRepo) FindByIDUnscoped(
	_ context.Context, companyID uuid.UUID,
) (*entity.Company, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	company, ok := r.byID[companyID]
	if !ok {
		return nil, apperror.NotFound("company not found").WithOp("fake.FindByIDUnscoped")
	}
	clone := *company
	return &clone, nil
}

// seed inserts a company directly, bypassing the service.
func (r *fakeCompanyRepo) seed(company entity.Company) *entity.Company {
	r.mu.Lock()
	defer r.mu.Unlock()

	if company.ID == uuid.Nil {
		r.seq++
		company.ID = uuid.MustParse(seqUUID(r.seq))
	}
	company.Code = entity.NormalizeCode(company.Code)
	if company.Status == "" {
		company.Status = entity.CompanyActive
	}
	company.CreatedAt = fixedNow()
	company.UpdatedAt = company.CreatedAt

	stored := company
	r.byID[company.ID] = &stored
	return &stored
}

// ---------- memberships ----------

type fakeMembershipRepo struct {
	mu     sync.Mutex
	byID   map[uuid.UUID]*entity.Membership
	seq    int
	failOn map[string]error
}

func newFakeMembershipRepo() *fakeMembershipRepo {
	return &fakeMembershipRepo{
		byID:   map[uuid.UUID]*entity.Membership{},
		failOn: map[string]error{},
	}
}

func (r *fakeMembershipRepo) fail(method string, err error) { r.failOn[method] = err }

func (r *fakeMembershipRepo) Create(_ context.Context, membership *entity.Membership) error {
	if err := r.failOn["Create"]; err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Mirrors ux_memberships_company_user.
	for _, existing := range r.byID {
		if existing.CompanyID == membership.CompanyID && existing.UserID == membership.UserID {
			return apperror.Conflict("duplicate membership").WithOp("fake.Create")
		}
	}

	if membership.ID == uuid.Nil {
		r.seq++
		membership.ID = uuid.MustParse(seqUUID(5000 + r.seq))
	}
	membership.CreatedAt = fixedNow()
	membership.UpdatedAt = membership.CreatedAt

	stored := *membership
	r.byID[membership.ID] = &stored
	return nil
}

func (r *fakeMembershipRepo) Update(_ context.Context, membership *entity.Membership) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.byID[membership.ID]; !ok {
		return apperror.NotFound("membership not found").WithOp("fake.Update")
	}
	stored := *membership
	r.byID[membership.ID] = &stored
	return nil
}

// findActive is the shared lookup used by both this fake and the company fake.
func (r *fakeMembershipRepo) findActive(userID, companyID uuid.UUID) *entity.Membership {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, m := range r.byID {
		if m.UserID == userID && m.CompanyID == companyID && m.CanAccess() {
			clone := *m
			return &clone
		}
	}
	return nil
}

func (r *fakeMembershipRepo) FindActiveByUserAndCompany(
	_ context.Context, userID, companyID uuid.UUID,
) (*entity.Membership, error) {
	if m := r.findActive(userID, companyID); m != nil {
		return m, nil
	}
	return nil, apperror.NotFound("membership not found").
		WithOp("fake.FindActiveByUserAndCompany")
}

func (r *fakeMembershipRepo) ListActiveByUser(
	_ context.Context, userID uuid.UUID,
) ([]entity.Membership, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	result := make([]entity.Membership, 0)
	for _, m := range r.byID {
		if m.UserID == userID && m.CanAccess() {
			result = append(result, *m)
		}
	}
	return result, nil
}

func (r *fakeMembershipRepo) FindByID(
	_ context.Context, membershipID, companyID uuid.UUID,
) (*entity.Membership, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	m, ok := r.byID[membershipID]
	// The companyID check is the tenant filter. A fake that omitted it would
	// make every cross-company isolation test pass vacuously.
	if !ok || m.CompanyID != companyID {
		return nil, apperror.NotFound("membership not found").WithOp("fake.FindByID")
	}
	clone := *m
	return &clone, nil
}

func (r *fakeMembershipRepo) ListByCompany(
	_ context.Context, companyID uuid.UUID, query dto.ListMembershipsQuery,
) (pagination.Page[entity.Membership], error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	matched := make([]entity.Membership, 0)
	for _, m := range r.byID {
		if m.CompanyID != companyID {
			continue
		}
		if query.Status != "" && string(m.Status) != query.Status {
			continue
		}
		if query.Role != "" && string(m.Role) != query.Role {
			continue
		}
		matched = append(matched, *m)
	}

	return pagination.NewPage(matched, query.Request, int64(len(matched))), nil
}

func (r *fakeMembershipRepo) FindByUserInCompany(
	_ context.Context, companyID, userID uuid.UUID,
) (*entity.Membership, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, m := range r.byID {
		if m.CompanyID == companyID && m.UserID == userID {
			clone := *m
			return &clone, nil
		}
	}
	return nil, apperror.NotFound("membership not found").WithOp("fake.FindByUserInCompany")
}

func (r *fakeMembershipRepo) Delete(_ context.Context, membershipID, companyID uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	m, ok := r.byID[membershipID]
	if !ok || m.CompanyID != companyID {
		return apperror.NotFound("membership not found").WithOp("fake.Delete")
	}
	delete(r.byID, membershipID)
	return nil
}

func (r *fakeMembershipRepo) CountOwners(_ context.Context, companyID uuid.UUID) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var count int64
	for _, m := range r.byID {
		if m.CompanyID == companyID && m.IsOwner() && m.CanAccess() {
			count++
		}
	}
	return count, nil
}

// seed inserts a membership directly, bypassing the service.
func (r *fakeMembershipRepo) seed(m entity.Membership) *entity.Membership {
	r.mu.Lock()
	defer r.mu.Unlock()

	if m.ID == uuid.Nil {
		r.seq++
		m.ID = uuid.MustParse(seqUUID(5000 + r.seq))
	}
	if m.Status == "" {
		m.Status = entity.MembershipActive
	}
	if m.Status == entity.MembershipActive && m.JoinedAt == nil {
		now := fixedNow()
		m.JoinedAt = &now
	}
	m.CreatedAt = fixedNow()
	m.UpdatedAt = m.CreatedAt

	stored := m
	r.byID[m.ID] = &stored
	return &stored
}

func (r *fakeMembershipRepo) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.byID)
}

// ---------- transaction manager ----------

// fakeTxManager simulates real transaction semantics, including ROLLBACK.
//
// It snapshots the fake repositories before running fn and restores them on
// error. Without this a failed flow would commit partial work in the fake, and
// a whole class of bug becomes invisible to unit tests — the auth sprint hit
// exactly that.
type fakeTxManager struct {
	companies   *fakeCompanyRepo
	memberships *fakeMembershipRepo

	calls     int
	rollbacks int
	depth     int
}

func (m *fakeTxManager) RunInTransaction(ctx context.Context, fn func(context.Context) error) error {
	m.calls++

	if m.depth > 0 {
		// Joined transaction: the outer snapshot already covers this work,
		// mirroring the savepoint behaviour of the real GormManager.
		return fn(ctx)
	}

	snapshot := m.snapshot()

	m.depth++
	err := fn(ctx)
	m.depth--

	if err != nil {
		m.rollbacks++
		m.restore(snapshot)
		return err
	}
	return nil
}

type txSnapshot struct {
	companies   map[uuid.UUID]entity.Company
	memberships map[uuid.UUID]entity.Membership
}

func (m *fakeTxManager) snapshot() txSnapshot {
	snap := txSnapshot{
		companies:   map[uuid.UUID]entity.Company{},
		memberships: map[uuid.UUID]entity.Membership{},
	}

	if m.companies != nil {
		m.companies.mu.Lock()
		for id, c := range m.companies.byID {
			snap.companies[id] = *c
		}
		m.companies.mu.Unlock()
	}
	if m.memberships != nil {
		m.memberships.mu.Lock()
		for id, mem := range m.memberships.byID {
			snap.memberships[id] = *mem
		}
		m.memberships.mu.Unlock()
	}

	return snap
}

func (m *fakeTxManager) restore(snap txSnapshot) {
	if m.companies != nil {
		m.companies.mu.Lock()
		m.companies.byID = map[uuid.UUID]*entity.Company{}
		for id, c := range snap.companies {
			clone := c
			m.companies.byID[id] = &clone
		}
		m.companies.mu.Unlock()
	}
	if m.memberships != nil {
		m.memberships.mu.Lock()
		m.memberships.byID = map[uuid.UUID]*entity.Membership{}
		for id, mem := range snap.memberships {
			clone := mem
			m.memberships.byID[id] = &clone
		}
		m.memberships.mu.Unlock()
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

// seqUUID renders a counter as a valid v4 UUID, mirroring adapter/id.Sequential
// so fake-assigned ids are predictable and well-formed.
func seqUUID(n int) string {
	return fmt.Sprintf("00000000-0000-4000-8000-%012d", n)
}

func zapNop() *zap.Logger { return zap.NewNop() }
