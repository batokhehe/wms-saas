package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/batokhehe/wms-saas/backend/internal/module/auth/entity"
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// In-memory doubles for the repositories, the transaction manager and the event
// publisher. Together they let every authentication flow be tested with no
// database, no Redis and no HTTP server — which is the practical payoff of
// keeping the service free of gin and gorm imports.

// ---------- users ----------

type fakeUserRepo struct {
	mu     sync.Mutex
	byID   map[uuid.UUID]*entity.User
	seq    int
	failOn map[string]error
}

func newFakeUserRepo() *fakeUserRepo {
	return &fakeUserRepo{
		byID:   map[uuid.UUID]*entity.User{},
		failOn: map[string]error{},
	}
}

func (r *fakeUserRepo) fail(method string, err error) { r.failOn[method] = err }

func (r *fakeUserRepo) Create(_ context.Context, user *entity.User) error {
	if err := r.failOn["Create"]; err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	for _, existing := range r.byID {
		if existing.Email == user.Email {
			return apperror.Conflict("duplicate email").WithOp("fake.Create")
		}
	}

	if user.ID == uuid.Nil {
		r.seq++
		user.ID = uuid.MustParse(sequentialUUID(r.seq))
	}
	user.CreatedAt = time.Now().UTC()
	user.UpdatedAt = user.CreatedAt

	stored := *user
	r.byID[user.ID] = &stored

	return nil
}

func (r *fakeUserRepo) Update(_ context.Context, user *entity.User) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.byID[user.ID]; !ok {
		return apperror.ErrNotFound
	}
	stored := *user
	r.byID[user.ID] = &stored
	return nil
}

func (r *fakeUserRepo) FindByID(_ context.Context, id uuid.UUID) (*entity.User, error) {
	if err := r.failOn["FindByID"]; err != nil {
		return nil, err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	user, ok := r.byID[id]
	if !ok {
		return nil, apperror.NotFound("user not found").WithOp("fake.FindByID")
	}
	clone := *user
	return &clone, nil
}

func (r *fakeUserRepo) FindByEmail(_ context.Context, email string) (*entity.User, error) {
	if err := r.failOn["FindByEmail"]; err != nil {
		return nil, err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	normalized := entity.NormalizeEmail(email)
	for _, user := range r.byID {
		if user.Email == normalized {
			clone := *user
			return &clone, nil
		}
	}
	return nil, apperror.NotFound("user not found").WithOp("fake.FindByEmail")
}

func (r *fakeUserRepo) ExistsByEmail(_ context.Context, email string) (bool, error) {
	if err := r.failOn["ExistsByEmail"]; err != nil {
		return false, err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	normalized := entity.NormalizeEmail(email)
	for _, user := range r.byID {
		if user.Email == normalized {
			return true, nil
		}
	}
	return false, nil
}

func (r *fakeUserRepo) UpdateLastLogin(_ context.Context, id uuid.UUID, at time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	user, ok := r.byID[id]
	if !ok {
		return apperror.ErrNotFound
	}
	user.LastLoginAt = &at
	return nil
}

// seed inserts a user directly, bypassing the service.
func (r *fakeUserRepo) seed(user entity.User) *entity.User {
	r.mu.Lock()
	defer r.mu.Unlock()

	if user.ID == uuid.Nil {
		r.seq++
		user.ID = uuid.MustParse(sequentialUUID(r.seq))
	}
	user.Email = entity.NormalizeEmail(user.Email)
	user.CreatedAt = time.Now().UTC()
	user.UpdatedAt = user.CreatedAt

	stored := user
	r.byID[user.ID] = &stored
	return &stored
}

// ---------- refresh tokens ----------

type fakeTokenRepo struct {
	mu     sync.Mutex
	byID   map[uuid.UUID]*entity.RefreshToken
	seq    int
	failOn map[string]error
}

func newFakeTokenRepo() *fakeTokenRepo {
	return &fakeTokenRepo{
		byID:   map[uuid.UUID]*entity.RefreshToken{},
		failOn: map[string]error{},
	}
}

func (r *fakeTokenRepo) fail(method string, err error) { r.failOn[method] = err }

func (r *fakeTokenRepo) Create(_ context.Context, token *entity.RefreshToken) error {
	if err := r.failOn["Create"]; err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if token.ID == uuid.Nil {
		r.seq++
		token.ID = uuid.MustParse(sequentialUUID(1000 + r.seq))
	}
	token.CreatedAt = time.Now().UTC().Add(time.Duration(r.seq) * time.Millisecond)
	token.UpdatedAt = token.CreatedAt

	stored := *token
	r.byID[token.ID] = &stored
	return nil
}

func (r *fakeTokenRepo) FindByHash(_ context.Context, hash string) (*entity.RefreshToken, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, token := range r.byID {
		if token.TokenHash == hash {
			clone := *token
			return &clone, nil
		}
	}
	return nil, apperror.NotFound("token not found").WithOp("fake.FindByHash")
}

func (r *fakeTokenRepo) Revoke(_ context.Context, id uuid.UUID, at time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	token, ok := r.byID[id]
	if !ok {
		return nil
	}
	token.Revoke(at)
	return nil
}

func (r *fakeTokenRepo) RevokeIfLive(
	_ context.Context, id uuid.UUID, at time.Time,
) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	token, ok := r.byID[id]
	if !ok || token.IsRevoked() {
		return false, nil
	}
	token.Revoke(at)
	return true, nil
}

func (r *fakeTokenRepo) RevokeAllForUser(
	_ context.Context, userID uuid.UUID, at time.Time,
) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var count int64
	for _, token := range r.byID {
		if token.UserID == userID && !token.IsRevoked() {
			token.Revoke(at)
			count++
		}
	}
	return count, nil
}

func (r *fakeTokenRepo) CountLiveForUser(
	_ context.Context, userID uuid.UUID, now time.Time,
) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var count int64
	for _, token := range r.byID {
		if token.UserID == userID && token.IsUsable(now) {
			count++
		}
	}
	return count, nil
}

func (r *fakeTokenRepo) OldestLiveForUser(
	_ context.Context, userID uuid.UUID, now time.Time,
) (*entity.RefreshToken, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var oldest *entity.RefreshToken
	for _, token := range r.byID {
		if token.UserID != userID || !token.IsUsable(now) {
			continue
		}
		if oldest == nil || token.CreatedAt.Before(oldest.CreatedAt) {
			oldest = token
		}
	}

	if oldest == nil {
		return nil, apperror.NotFound("no live token").WithOp("fake.OldestLiveForUser")
	}
	clone := *oldest
	return &clone, nil
}

func (r *fakeTokenRepo) DeleteExpiredBefore(_ context.Context, cutoff time.Time) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var count int64
	for id, token := range r.byID {
		if token.ExpiresAt.Before(cutoff) {
			delete(r.byID, id)
			count++
		}
	}
	return count, nil
}

func (r *fakeTokenRepo) liveCount(userID uuid.UUID, now time.Time) int {
	count, _ := r.CountLiveForUser(context.Background(), userID, now)
	return int(count)
}

// ---------- transaction manager ----------

// fakeTxManager simulates real transaction semantics, including ROLLBACK.
//
// It snapshots the fake repositories before running fn and restores them if fn
// returns an error. Without this the fake would commit partial work on a failed
// flow, and a whole class of bug becomes invisible to unit tests — including
// the real one this suite now catches: performing a security-critical write
// inside a transaction that is about to fail, so the rollback undoes it.
type fakeTxManager struct {
	users  *fakeUserRepo
	tokens *fakeTokenRepo

	calls     int
	rollbacks int
	// depth tracks nesting so an inner call joins the outer transaction rather
	// than taking its own snapshot, mirroring the savepoint behaviour of the
	// real GormManager.
	depth int
}

func (m *fakeTxManager) RunInTransaction(ctx context.Context, fn func(context.Context) error) error {
	m.calls++

	if m.depth > 0 {
		// Joined transaction: the outer snapshot already covers this work.
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
	users  map[uuid.UUID]entity.User
	tokens map[uuid.UUID]entity.RefreshToken
}

func (m *fakeTxManager) snapshot() txSnapshot {
	snap := txSnapshot{
		users:  map[uuid.UUID]entity.User{},
		tokens: map[uuid.UUID]entity.RefreshToken{},
	}

	if m.users != nil {
		m.users.mu.Lock()
		for id, user := range m.users.byID {
			snap.users[id] = *user
		}
		m.users.mu.Unlock()
	}
	if m.tokens != nil {
		m.tokens.mu.Lock()
		for id, token := range m.tokens.byID {
			snap.tokens[id] = *token
		}
		m.tokens.mu.Unlock()
	}

	return snap
}

func (m *fakeTxManager) restore(snap txSnapshot) {
	if m.users != nil {
		m.users.mu.Lock()
		m.users.byID = map[uuid.UUID]*entity.User{}
		for id, user := range snap.users {
			clone := user
			m.users.byID[id] = &clone
		}
		m.users.mu.Unlock()
	}
	if m.tokens != nil {
		m.tokens.mu.Lock()
		m.tokens.byID = map[uuid.UUID]*entity.RefreshToken{}
		for id, token := range snap.tokens {
			clone := token
			m.tokens.byID[id] = &clone
		}
		m.tokens.mu.Unlock()
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

// sequentialUUID renders a counter as a valid v4 UUID, mirroring
// adapter/id.Sequential so fake-assigned ids are predictable and well-formed.
func sequentialUUID(n int) string {
	return fmt.Sprintf("00000000-0000-4000-8000-%012d", n)
}

// zapNop returns a discard logger for tests that need a RequestContext.
func zapNop() *zap.Logger { return zap.NewNop() }

// toString renders an attribute value for the secret-leak assertions.
func toString(value any) string { return fmt.Sprintf("%v", value) }
