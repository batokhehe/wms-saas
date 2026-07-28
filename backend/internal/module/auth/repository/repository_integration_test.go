//go:build integration

// Repository tests run against a real PostgreSQL instance.
//
// They are behind a build tag because they need a live database with the
// migrations applied. Unit tests must never require infrastructure — see
// CodingStandard.md — so these are opted into explicitly:
//
//	make test-integration
//
// What they verify cannot be verified any other way: the partial unique index,
// GORM's soft-delete filtering, error translation from real driver errors, and
// the interaction between the repository and a real transaction.
package repository

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/batokhehe/wms-saas/backend/internal/module/auth/entity"
	adapterid "github.com/batokhehe/wms-saas/backend/internal/shared/adapter/id"
	"github.com/batokhehe/wms-saas/backend/internal/shared/transaction"
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

func testDSN() string {
	if dsn := os.Getenv("TEST_DATABASE_DSN"); dsn != "" {
		return dsn
	}
	return "host=localhost port=5432 user=wms password=wms dbname=wms sslmode=disable TimeZone=UTC"
}

func openDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(postgres.Open(testDSN()), &gorm.Config{
		TranslateError: true,
		NowFunc:        func() time.Time { return time.Now().UTC() },
	})
	if err != nil {
		t.Fatalf("connecting to the test database: %v", err)
	}

	return db
}

// newRepos returns both repositories over a clean slate.
func newRepos(t *testing.T) (*gorm.DB, UserRepository, RefreshTokenRepository) {
	t.Helper()

	db := openDB(t)
	ids := adapterid.NewUUID()

	// Order matters: refresh_tokens holds the foreign key.
	db.Exec("DELETE FROM refresh_tokens")
	db.Exec("DELETE FROM users")

	t.Cleanup(func() {
		db.Exec("DELETE FROM refresh_tokens")
		db.Exec("DELETE FROM users")
	})

	return db, NewUserRepository(db, ids), NewRefreshTokenRepository(db, ids)
}

func newUser(email string) *entity.User {
	return &entity.User{
		Email:        entity.NormalizeEmail(email),
		PasswordHash: "$2a$12$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy",
		FullName:     "Test User",
		Status:       entity.StatusActive,
	}
}

// ---------- users ----------

func TestUserCreateAssignsID(t *testing.T) {
	_, users, _ := newRepos(t)
	ctx := context.Background()

	user := newUser("ops@example.com")
	if err := users.Create(ctx, user); err != nil {
		t.Fatalf("Create() = %v", err)
	}

	if user.ID == uuid.Nil {
		t.Error("Create() did not assign an ID from the generator")
	}
	if user.CreatedAt.IsZero() {
		t.Error("CreatedAt was not set")
	}
}

// TestUserEmailIsCaseInsensitive exercises the CITEXT column: an operator who
// registered as "Ops@example.com" must be able to log in as "ops@example.com".
func TestUserEmailIsCaseInsensitive(t *testing.T) {
	_, users, _ := newRepos(t)
	ctx := context.Background()

	if err := users.Create(ctx, newUser("Ops@Example.com")); err != nil {
		t.Fatalf("Create() = %v", err)
	}

	for _, variant := range []string{"ops@example.com", "OPS@EXAMPLE.COM", "Ops@Example.com"} {
		found, err := users.FindByEmail(ctx, variant)
		if err != nil {
			t.Errorf("FindByEmail(%q) = %v", variant, err)
			continue
		}
		if found.Email != "ops@example.com" {
			t.Errorf("stored email = %q, want the normalised form", found.Email)
		}
	}
}

func TestUserDuplicateEmailIsConflict(t *testing.T) {
	_, users, _ := newRepos(t)
	ctx := context.Background()

	if err := users.Create(ctx, newUser("ops@example.com")); err != nil {
		t.Fatalf("first Create() = %v", err)
	}

	// Different case: the CITEXT unique index must still reject it.
	err := users.Create(ctx, newUser("OPS@EXAMPLE.COM"))
	if err == nil {
		t.Fatal("duplicate Create() = nil, want CONFLICT")
	}
	if code := apperror.From(err).Code; code != apperror.CodeConflict {
		t.Errorf("code = %s, want CONFLICT", code)
	}
}

// TestUserEmailReusableAfterSoftDelete proves the unique index is partial. A
// plain UNIQUE would let a deleted account permanently reserve its address.
func TestUserEmailReusableAfterSoftDelete(t *testing.T) {
	db, users, _ := newRepos(t)
	ctx := context.Background()

	original := newUser("ops@example.com")
	if err := users.Create(ctx, original); err != nil {
		t.Fatalf("Create() = %v", err)
	}

	// Soft delete directly; the auth service exposes no account deletion.
	if err := db.Delete(&entity.User{}, "id = ?", original.ID).Error; err != nil {
		t.Fatalf("soft delete: %v", err)
	}

	// GORM must now hide it.
	if _, err := users.FindByEmail(ctx, "ops@example.com"); err == nil {
		t.Error("a soft-deleted user was still returned by FindByEmail")
	}

	if err := users.Create(ctx, newUser("ops@example.com")); err != nil {
		t.Errorf("re-registering a soft-deleted address = %v, want nil", err)
	}
}

func TestUserFindByEmailNotFound(t *testing.T) {
	_, users, _ := newRepos(t)

	_, err := users.FindByEmail(context.Background(), "nobody@example.com")
	if err == nil {
		t.Fatal("FindByEmail() = nil for an unknown address")
	}
	if code := apperror.From(err).Code; code != apperror.CodeNotFound {
		t.Errorf("code = %s, want NOT_FOUND", code)
	}
}

// TestUpdateLastLoginDoesNotClobber is why UpdateLastLogin is a targeted UPDATE
// rather than a full Save: a concurrent password change must not be reverted by
// a login that started before it.
func TestUpdateLastLoginDoesNotClobber(t *testing.T) {
	db, users, _ := newRepos(t)
	ctx := context.Background()

	user := newUser("ops@example.com")
	if err := users.Create(ctx, user); err != nil {
		t.Fatalf("Create() = %v", err)
	}

	// Simulate a password change landing between the read and the login stamp.
	newHash := "$2a$12$abcdefghijklmnopqrstuv0123456789012345678901234567890"
	if err := db.Model(&entity.User{}).
		Where("id = ?", user.ID).
		Update("password_hash", newHash).Error; err != nil {
		t.Fatalf("simulating password change: %v", err)
	}

	if err := users.UpdateLastLogin(ctx, user.ID, time.Now().UTC()); err != nil {
		t.Fatalf("UpdateLastLogin() = %v", err)
	}

	after, err := users.FindByID(ctx, user.ID)
	if err != nil {
		t.Fatalf("FindByID() = %v", err)
	}
	if after.PasswordHash != newHash {
		t.Error("UpdateLastLogin reverted a concurrent password change")
	}
	if after.LastLoginAt == nil {
		t.Error("last_login_at was not recorded")
	}
}

func TestUserExistsByEmail(t *testing.T) {
	_, users, _ := newRepos(t)
	ctx := context.Background()

	if err := users.Create(ctx, newUser("ops@example.com")); err != nil {
		t.Fatalf("Create() = %v", err)
	}

	exists, err := users.ExistsByEmail(ctx, "OPS@example.com")
	if err != nil || !exists {
		t.Errorf("ExistsByEmail(existing) = %v, %v; want true, nil", exists, err)
	}

	missing, err := users.ExistsByEmail(ctx, "nobody@example.com")
	if err != nil || missing {
		t.Errorf("ExistsByEmail(unknown) = %v, %v; want false, nil", missing, err)
	}
}

// ---------- refresh tokens ----------

func seedToken(t *testing.T, tokens RefreshTokenRepository, userID uuid.UUID, hash string, expiresAt time.Time) *entity.RefreshToken {
	t.Helper()

	token := &entity.RefreshToken{
		UserID:    userID,
		TokenHash: hash,
		ExpiresAt: expiresAt,
		Device:    "Test Device",
	}
	if err := tokens.Create(context.Background(), token); err != nil {
		t.Fatalf("Create() = %v", err)
	}
	return token
}

func TestRefreshTokenLifecycle(t *testing.T) {
	_, users, tokens := newRepos(t)
	ctx := context.Background()

	user := newUser("ops@example.com")
	if err := users.Create(ctx, user); err != nil {
		t.Fatalf("Create(user) = %v", err)
	}

	now := time.Now().UTC()
	hash := "a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
	token := seedToken(t, tokens, user.ID, hash, now.Add(time.Hour))

	found, err := tokens.FindByHash(ctx, hash)
	if err != nil {
		t.Fatalf("FindByHash() = %v", err)
	}
	if found.ID != token.ID {
		t.Errorf("found id = %s, want %s", found.ID, token.ID)
	}
	if !found.IsUsable(now) {
		t.Error("a fresh token reported not usable")
	}

	if err := tokens.Revoke(ctx, token.ID, now); err != nil {
		t.Fatalf("Revoke() = %v", err)
	}

	// Still findable after revocation — reuse detection depends on it.
	revoked, err := tokens.FindByHash(ctx, hash)
	if err != nil {
		t.Fatalf("FindByHash() after revoke = %v", err)
	}
	if !revoked.IsRevoked() {
		t.Error("the token was not marked revoked")
	}
}

// TestRevokeIsIdempotent: re-revoking preserves the original timestamp, so the
// audit trail records when the session actually ended.
func TestRevokeIsIdempotent(t *testing.T) {
	_, users, tokens := newRepos(t)
	ctx := context.Background()

	user := newUser("ops@example.com")
	users.Create(ctx, user)

	now := time.Now().UTC()
	hash := "b1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
	token := seedToken(t, tokens, user.ID, hash, now.Add(time.Hour))

	if err := tokens.Revoke(ctx, token.ID, now); err != nil {
		t.Fatalf("first Revoke() = %v", err)
	}
	first, _ := tokens.FindByHash(ctx, hash)

	if err := tokens.Revoke(ctx, token.ID, now.Add(time.Hour)); err != nil {
		t.Fatalf("second Revoke() = %v", err)
	}
	second, _ := tokens.FindByHash(ctx, hash)

	if !first.RevokedAt.Equal(*second.RevokedAt) {
		t.Errorf("revoked_at changed on re-revoke: %v -> %v", first.RevokedAt, second.RevokedAt)
	}
}

func TestRevokeAllForUser(t *testing.T) {
	_, users, tokens := newRepos(t)
	ctx := context.Background()

	user := newUser("ops@example.com")
	users.Create(ctx, user)
	other := newUser("other@example.com")
	users.Create(ctx, other)

	now := time.Now().UTC()
	for i, hash := range []string{
		"c100000000000000000000000000000000000000000000000000000000000001",
		"c100000000000000000000000000000000000000000000000000000000000002",
		"c100000000000000000000000000000000000000000000000000000000000003",
	} {
		_ = i
		seedToken(t, tokens, user.ID, hash, now.Add(time.Hour))
	}
	seedToken(t, tokens, other.ID,
		"d100000000000000000000000000000000000000000000000000000000000001", now.Add(time.Hour))

	revoked, err := tokens.RevokeAllForUser(ctx, user.ID, now)
	if err != nil {
		t.Fatalf("RevokeAllForUser() = %v", err)
	}
	if revoked != 3 {
		t.Errorf("revoked = %d, want 3", revoked)
	}

	live, _ := tokens.CountLiveForUser(ctx, user.ID, now)
	if live != 0 {
		t.Errorf("live sessions = %d, want 0", live)
	}

	// Another user's session must be untouched.
	otherLive, _ := tokens.CountLiveForUser(ctx, other.ID, now)
	if otherLive != 1 {
		t.Errorf("other user's live sessions = %d, want 1", otherLive)
	}
}

func TestCountLiveExcludesExpiredAndRevoked(t *testing.T) {
	_, users, tokens := newRepos(t)
	ctx := context.Background()

	user := newUser("ops@example.com")
	users.Create(ctx, user)

	now := time.Now().UTC()

	seedToken(t, tokens, user.ID,
		"e100000000000000000000000000000000000000000000000000000000000001", now.Add(time.Hour))
	seedToken(t, tokens, user.ID,
		"e100000000000000000000000000000000000000000000000000000000000002", now.Add(-time.Hour))
	revokedToken := seedToken(t, tokens, user.ID,
		"e100000000000000000000000000000000000000000000000000000000000003", now.Add(time.Hour))
	tokens.Revoke(ctx, revokedToken.ID, now)

	live, err := tokens.CountLiveForUser(ctx, user.ID, now)
	if err != nil {
		t.Fatalf("CountLiveForUser() = %v", err)
	}
	if live != 1 {
		t.Errorf("live = %d, want 1 (expired and revoked must be excluded)", live)
	}
}

func TestOldestLiveForUser(t *testing.T) {
	_, users, tokens := newRepos(t)
	ctx := context.Background()

	user := newUser("ops@example.com")
	users.Create(ctx, user)

	now := time.Now().UTC()

	first := seedToken(t, tokens, user.ID,
		"f100000000000000000000000000000000000000000000000000000000000001", now.Add(time.Hour))
	time.Sleep(10 * time.Millisecond) // ensure a distinct created_at
	seedToken(t, tokens, user.ID,
		"f100000000000000000000000000000000000000000000000000000000000002", now.Add(time.Hour))

	oldest, err := tokens.OldestLiveForUser(ctx, user.ID, now)
	if err != nil {
		t.Fatalf("OldestLiveForUser() = %v", err)
	}
	if oldest.ID != first.ID {
		t.Errorf("oldest = %s, want the first-created %s", oldest.ID, first.ID)
	}
}

func TestDuplicateTokenHashIsConflict(t *testing.T) {
	_, users, tokens := newRepos(t)
	ctx := context.Background()

	user := newUser("ops@example.com")
	users.Create(ctx, user)

	now := time.Now().UTC()
	hash := "aa00000000000000000000000000000000000000000000000000000000000001"

	seedToken(t, tokens, user.ID, hash, now.Add(time.Hour))

	// A hash collision would let one session authenticate as another, so the
	// database enforces uniqueness rather than trusting the generator.
	err := tokens.Create(ctx, &entity.RefreshToken{
		UserID:    user.ID,
		TokenHash: hash,
		ExpiresAt: now.Add(time.Hour),
	})
	if err == nil {
		t.Fatal("a duplicate token hash was accepted")
	}
	if code := apperror.From(err).Code; code != apperror.CodeConflict {
		t.Errorf("code = %s, want CONFLICT", code)
	}
}

func TestDeleteExpiredBefore(t *testing.T) {
	_, users, tokens := newRepos(t)
	ctx := context.Background()

	user := newUser("ops@example.com")
	users.Create(ctx, user)

	now := time.Now().UTC()

	seedToken(t, tokens, user.ID,
		"bb00000000000000000000000000000000000000000000000000000000000001", now.Add(-48*time.Hour))
	seedToken(t, tokens, user.ID,
		"bb00000000000000000000000000000000000000000000000000000000000002", now.Add(time.Hour))

	deleted, err := tokens.DeleteExpiredBefore(ctx, now)
	if err != nil {
		t.Fatalf("DeleteExpiredBefore() = %v", err)
	}
	if deleted != 1 {
		t.Errorf("deleted = %d, want 1", deleted)
	}

	// The live one survives.
	if _, err := tokens.FindByHash(ctx,
		"bb00000000000000000000000000000000000000000000000000000000000002"); err != nil {
		t.Errorf("the live token was deleted: %v", err)
	}
}

// TestRepositoriesEnrolInTransaction proves the repositories pick up the
// transaction from the context with no explicit plumbing — and that a rollback
// undoes every write in the unit of work.
func TestRepositoriesEnrolInTransaction(t *testing.T) {
	db, users, tokens := newRepos(t)
	ctx := context.Background()
	manager := transaction.NewGormManager(db)

	sentinel := apperror.Internal("forced rollback")

	err := manager.RunInTransaction(ctx, func(txCtx context.Context) error {
		user := newUser("rollback@example.com")
		if err := users.Create(txCtx, user); err != nil {
			return err
		}
		if err := tokens.Create(txCtx, &entity.RefreshToken{
			UserID:    user.ID,
			TokenHash: "cc00000000000000000000000000000000000000000000000000000000000001",
			ExpiresAt: time.Now().UTC().Add(time.Hour),
		}); err != nil {
			return err
		}
		return sentinel
	})
	if err == nil {
		t.Fatal("RunInTransaction() = nil despite the callback failing")
	}

	if _, err := users.FindByEmail(ctx, "rollback@example.com"); err == nil {
		t.Error("the user survived a rolled-back transaction")
	}
	if _, err := tokens.FindByHash(ctx,
		"cc00000000000000000000000000000000000000000000000000000000000001"); err == nil {
		t.Error("the refresh token survived a rolled-back transaction")
	}
}
