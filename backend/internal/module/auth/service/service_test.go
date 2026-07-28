package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/batokhehe/wms-saas/backend/internal/module/auth/dto"
	"github.com/batokhehe/wms-saas/backend/internal/module/auth/entity"
	adapterclock "github.com/batokhehe/wms-saas/backend/internal/shared/adapter/clock"
	adapterid "github.com/batokhehe/wms-saas/backend/internal/shared/adapter/id"
	"github.com/batokhehe/wms-saas/backend/internal/shared/appcontext"
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

const validPassword = "Str0ng!Passw0rd"

type harness struct {
	svc    *Service
	users  *fakeUserRepo
	tokens *fakeTokenRepo
	tx     *fakeTxManager
	events *fakeEventPublisher
	clock  *adapterclock.Fake
	hasher PasswordHasher
}

func newHarness(t *testing.T) *harness {
	t.Helper()

	users := newFakeUserRepo()
	tokens := newFakeTokenRepo()
	tx := &fakeTxManager{users: users, tokens: tokens}
	events := &fakeEventPublisher{}
	clock := adapterclock.NewFakeAt("2026-07-22T10:00:00Z")
	hasher := NewBcryptHasher(testCost)

	tokenSvc := NewTokenService(testJWTConfig(), clock, adapterid.NewSequential())

	return &harness{
		svc:    New(users, tokens, hasher, tokenSvc, clock, tx, events, 3),
		users:  users,
		tokens: tokens,
		tx:     tx,
		events: events,
		clock:  clock,
		hasher: hasher,
	}
}

// seedUser inserts an active account with a known password.
func (h *harness) seedUser(t *testing.T, email string) *entity.User {
	t.Helper()

	hash, err := h.hasher.Hash(validPassword)
	if err != nil {
		t.Fatalf("hashing seed password: %v", err)
	}

	return h.users.seed(entity.User{
		Email:        email,
		PasswordHash: hash,
		FullName:     "Seed User",
		Status:       entity.StatusActive,
	})
}

// authedContext builds a context carrying an authenticated principal, standing
// in for what the JWT middleware does.
func authedContext(user *entity.User) context.Context {
	rc := appcontext.New("test-request", zapNop())
	rc.WithTenant(nil, &user.ID, "")
	return appcontext.Into(context.Background(), rc)
}

// ---------- Register ----------

func TestRegisterCreatesAccountAndTokens(t *testing.T) {
	h := newHarness(t)

	got, err := h.svc.Register(context.Background(), dto.RegisterRequest{
		Email:    "Ops@Example.com",
		Password: validPassword,
		FullName: "Ops User",
	}, SessionContext{IPAddress: "10.0.0.1"})
	if err != nil {
		t.Fatalf("Register() = %v", err)
	}

	// The email is normalised on the way in, so the stored form is canonical.
	if got.User.Email != "ops@example.com" {
		t.Errorf("email = %q, want it normalised to lowercase", got.User.Email)
	}
	if got.User.Status != string(entity.StatusActive) {
		t.Errorf("status = %q, want ACTIVE", got.User.Status)
	}
	if got.Tokens.AccessToken == "" || got.Tokens.RefreshToken == "" {
		t.Error("Register() returned an empty token")
	}
	if got.Tokens.TokenType != "Bearer" {
		t.Errorf("token_type = %q, want Bearer", got.Tokens.TokenType)
	}
	if !h.events.has(entity.EventUserRegistered) {
		t.Errorf("UserRegistered not published; got %v", h.events.names())
	}
}

// TestRegisterStoresOnlyTheHash is the core credential-handling guarantee.
func TestRegisterStoresOnlyTheHash(t *testing.T) {
	h := newHarness(t)

	if _, err := h.svc.Register(context.Background(), dto.RegisterRequest{
		Email:    "ops@example.com",
		Password: validPassword,
		FullName: "Ops User",
	}, SessionContext{}); err != nil {
		t.Fatalf("Register() = %v", err)
	}

	stored, err := h.users.FindByEmail(context.Background(), "ops@example.com")
	if err != nil {
		t.Fatalf("FindByEmail() = %v", err)
	}

	if stored.PasswordHash == validPassword {
		t.Fatal("the raw password was stored instead of a hash")
	}
	if !strings.HasPrefix(stored.PasswordHash, "$2") {
		t.Errorf("stored value %q is not a bcrypt hash", stored.PasswordHash)
	}
	if !h.hasher.Verify(stored.PasswordHash, validPassword) {
		t.Error("the stored hash does not verify against the original password")
	}
}

// TestRegisterStoresOnlyRefreshTokenHash: the raw refresh token is returned to
// the caller exactly once and never persisted.
func TestRegisterStoresOnlyRefreshTokenHash(t *testing.T) {
	h := newHarness(t)

	got, err := h.svc.Register(context.Background(), dto.RegisterRequest{
		Email:    "ops@example.com",
		Password: validPassword,
		FullName: "Ops User",
	}, SessionContext{})
	if err != nil {
		t.Fatalf("Register() = %v", err)
	}

	raw := got.Tokens.RefreshToken

	for _, stored := range h.tokens.byID {
		if stored.TokenHash == raw {
			t.Fatal("the raw refresh token was stored")
		}
		if stored.TokenHash != HashRefreshToken(raw) {
			t.Errorf("stored hash = %q, want the SHA-256 of the raw token", stored.TokenHash)
		}
	}
}

func TestRegisterRejectsDuplicateEmail(t *testing.T) {
	h := newHarness(t)
	h.seedUser(t, "ops@example.com")

	// Different case: the normalisation must still catch it.
	_, err := h.svc.Register(context.Background(), dto.RegisterRequest{
		Email:    "OPS@EXAMPLE.COM",
		Password: validPassword,
		FullName: "Impostor",
	}, SessionContext{})

	if err == nil {
		t.Fatal("Register() = nil for a duplicate email")
	}
	if code := apperror.From(err).Code; code != apperror.CodeConflict {
		t.Errorf("code = %s, want CONFLICT", code)
	}
}

func TestRegisterRejectsWeakPassword(t *testing.T) {
	h := newHarness(t)

	tests := map[string]string{
		"too short":     "Ab1!",
		"no uppercase":  "str0ng!passw0rd",
		"no lowercase":  "STR0NG!PASSW0RD",
		"no digit":      "Strong!Password",
		"no special":    "Str0ngPassw0rd1",
		"only spaces":   "        ",
		"over 72 bytes": strings.Repeat("Aa1!", 20),
	}

	for name, password := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := h.svc.Register(context.Background(), dto.RegisterRequest{
				Email:    "new@example.com",
				Password: password,
				FullName: "User",
			}, SessionContext{})

			if err == nil {
				t.Fatalf("Register() = nil for password %q", password)
			}
			if code := apperror.From(err).Code; code != apperror.CodeValidation {
				t.Errorf("code = %s, want VALIDATION_ERROR", code)
			}
		})
	}
}

// TestRegisterRollsBackOnTokenFailure proves the flow is atomic: a failure
// after the user row is written must not leave an account behind whose caller
// believes registration failed.
func TestRegisterRollsBackOnTokenFailure(t *testing.T) {
	h := newHarness(t)
	h.tokens.fail("Create", errors.New("session store unavailable"))

	_, err := h.svc.Register(context.Background(), dto.RegisterRequest{
		Email:    "ops@example.com",
		Password: validPassword,
		FullName: "Ops User",
	}, SessionContext{})

	if err == nil {
		t.Fatal("Register() = nil despite the session store failing")
	}
	if h.tx.rollbacks != 1 {
		t.Errorf("rollbacks = %d, want 1", h.tx.rollbacks)
	}

	// The account must be gone. Otherwise the caller believes registration
	// failed while the email is now taken, and their retry fails with a
	// conflict they cannot resolve.
	if _, err := h.users.FindByEmail(context.Background(), "ops@example.com"); err == nil {
		t.Error("the user survived a rolled-back registration")
	}
}

// TestReuseRevocationSurvivesTheFailedRequest is a regression test for a real
// defect found in end-to-end testing.
//
// Reuse detection revokes every session and then fails the request. The first
// implementation did both inside one transaction, so returning the auth error
// rolled the revocation back — the attacker's stolen session stayed alive,
// which is the exact opposite of the intended behaviour. The revocation now
// runs in its own committing transaction.
func TestReuseRevocationSurvivesTheFailedRequest(t *testing.T) {
	h := newHarness(t)
	user := h.seedUser(t, "ops@example.com")

	login, _ := h.svc.Login(context.Background(), dto.LoginRequest{
		Email: "ops@example.com", Password: validPassword,
	}, SessionContext{})

	stolen := login.Tokens.RefreshToken

	// The legitimate client rotates, producing a live successor session.
	rotated, err := h.svc.Refresh(context.Background(),
		dto.RefreshRequest{RefreshToken: stolen}, SessionContext{})
	if err != nil {
		t.Fatalf("Refresh() = %v", err)
	}

	// The attacker replays the stolen token. The request MUST fail...
	if _, err := h.svc.Refresh(context.Background(),
		dto.RefreshRequest{RefreshToken: stolen}, SessionContext{}); err == nil {
		t.Fatal("replaying a rotated token succeeded")
	}

	// ...and the revocation must have committed despite that failure.
	if got := h.tokens.liveCount(user.ID, h.clock.Now()); got != 0 {
		t.Errorf("live sessions = %d, want 0; the revocation was rolled back", got)
	}

	// The successor session the legitimate client is holding is dead too.
	if _, err := h.svc.Refresh(context.Background(),
		dto.RefreshRequest{RefreshToken: rotated.Tokens.RefreshToken}, SessionContext{}); err == nil {
		t.Error("the successor session survived reuse detection")
	}
}

// ---------- Login ----------

func TestLoginSucceeds(t *testing.T) {
	h := newHarness(t)
	user := h.seedUser(t, "ops@example.com")

	got, err := h.svc.Login(context.Background(), dto.LoginRequest{
		Email:    "ops@example.com",
		Password: validPassword,
		Device:   "Scanner 12",
	}, SessionContext{IPAddress: "10.0.0.1", UserAgent: "Zebra/1.0"})
	if err != nil {
		t.Fatalf("Login() = %v", err)
	}

	if got.Tokens.AccessToken == "" {
		t.Error("no access token returned")
	}
	if !h.events.has(entity.EventUserLoggedIn) {
		t.Errorf("UserLoggedIn not published; got %v", h.events.names())
	}

	stored, _ := h.users.FindByID(context.Background(), user.ID)
	if stored.LastLoginAt == nil {
		t.Error("last_login_at was not recorded")
	}

	// Provenance is captured against the session for the "active sessions" view.
	for _, token := range h.tokens.byID {
		if token.Device != "Scanner 12" {
			t.Errorf("device = %q, want %q", token.Device, "Scanner 12")
		}
		if token.IPAddress == nil || *token.IPAddress != "10.0.0.1" {
			t.Errorf("ip = %v, want 10.0.0.1", token.IPAddress)
		}
	}
}

// TestLoginFailuresAreIndistinguishable is the account-enumeration defence:
// an unknown address and a wrong password must produce the identical error.
func TestLoginFailuresAreIndistinguishable(t *testing.T) {
	h := newHarness(t)
	h.seedUser(t, "ops@example.com")

	_, unknownErr := h.svc.Login(context.Background(), dto.LoginRequest{
		Email:    "nobody@example.com",
		Password: validPassword,
	}, SessionContext{})

	_, wrongErr := h.svc.Login(context.Background(), dto.LoginRequest{
		Email:    "ops@example.com",
		Password: "Wr0ng!Passw0rd",
	}, SessionContext{})

	if unknownErr == nil || wrongErr == nil {
		t.Fatal("both logins should have failed")
	}

	unknown, wrong := apperror.From(unknownErr), apperror.From(wrongErr)

	if unknown.Code != wrong.Code {
		t.Errorf("codes differ: %s vs %s — this leaks whether the account exists",
			unknown.Code, wrong.Code)
	}
	if unknown.Message != wrong.Message {
		t.Errorf("messages differ: %q vs %q — this leaks whether the account exists",
			unknown.Message, wrong.Message)
	}
	if unknown.Code != apperror.CodeUnauthorized {
		t.Errorf("code = %s, want UNAUTHORIZED", unknown.Code)
	}
}

func TestLoginRejectsInactiveAndLockedAccounts(t *testing.T) {
	tests := map[entity.Status]string{
		entity.StatusLocked:   "locked",
		entity.StatusInactive: "inactive",
	}

	for status := range tests {
		t.Run(string(status), func(t *testing.T) {
			h := newHarness(t)

			hash, _ := h.hasher.Hash(validPassword)
			h.users.seed(entity.User{
				Email:        "ops@example.com",
				PasswordHash: hash,
				FullName:     "Ops",
				Status:       status,
			})

			_, err := h.svc.Login(context.Background(), dto.LoginRequest{
				Email:    "ops@example.com",
				Password: validPassword,
			}, SessionContext{})

			if err == nil {
				t.Fatalf("Login() = nil for a %s account", status)
			}
			// FORBIDDEN, not UNAUTHORIZED: the caller proved they know the
			// password, so telling them the account is disabled reveals nothing
			// an attacker could use.
			if code := apperror.From(err).Code; code != apperror.CodeForbidden {
				t.Errorf("code = %s, want FORBIDDEN", code)
			}
		})
	}
}

// TestLoginDoesNotRevokeOtherSessions: an operator with a scanner, a desktop
// and a phone must not be signed out of the others by logging in on one.
func TestLoginDoesNotRevokeOtherSessions(t *testing.T) {
	h := newHarness(t)
	user := h.seedUser(t, "ops@example.com")

	for i := 0; i < 2; i++ {
		if _, err := h.svc.Login(context.Background(), dto.LoginRequest{
			Email:    "ops@example.com",
			Password: validPassword,
		}, SessionContext{}); err != nil {
			t.Fatalf("Login() = %v", err)
		}
	}

	if got := h.tokens.liveCount(user.ID, h.clock.Now()); got != 2 {
		t.Errorf("live sessions = %d, want 2", got)
	}
}

// TestLoginEnforcesSessionCap: at the cap, the oldest session is evicted rather
// than the login being refused.
func TestLoginEnforcesSessionCap(t *testing.T) {
	h := newHarness(t) // cap is 3
	user := h.seedUser(t, "ops@example.com")

	for i := 0; i < 5; i++ {
		if _, err := h.svc.Login(context.Background(), dto.LoginRequest{
			Email:    "ops@example.com",
			Password: validPassword,
		}, SessionContext{}); err != nil {
			t.Fatalf("login %d = %v", i, err)
		}
	}

	if got := h.tokens.liveCount(user.ID, h.clock.Now()); got > 3 {
		t.Errorf("live sessions = %d, want at most the cap of 3", got)
	}
}

// ---------- Refresh / rotation ----------

func TestRefreshRotatesToken(t *testing.T) {
	h := newHarness(t)
	h.seedUser(t, "ops@example.com")

	login, err := h.svc.Login(context.Background(), dto.LoginRequest{
		Email:    "ops@example.com",
		Password: validPassword,
	}, SessionContext{})
	if err != nil {
		t.Fatalf("Login() = %v", err)
	}

	original := login.Tokens.RefreshToken

	// Advance so the new access token differs from the old one; without this
	// the identical iat/exp would produce a byte-identical JWT.
	h.clock.Advance(time.Minute)

	refreshed, err := h.svc.Refresh(context.Background(),
		dto.RefreshRequest{RefreshToken: original}, SessionContext{})
	if err != nil {
		t.Fatalf("Refresh() = %v", err)
	}

	if refreshed.Tokens.RefreshToken == original {
		t.Error("the refresh token was not rotated")
	}
	if refreshed.Tokens.AccessToken == login.Tokens.AccessToken {
		t.Error("the access token was not reissued")
	}
	if !h.events.has(entity.EventRefreshTokenRotated) {
		t.Errorf("RefreshTokenRotated not published; got %v", h.events.names())
	}

	// The old token must now be dead.
	old, _ := h.tokens.FindByHash(context.Background(), HashRefreshToken(original))
	if !old.IsRevoked() {
		t.Error("the rotated token was not revoked")
	}
}

// TestRefreshDetectsReuse is the highest-value security property of rotation:
// presenting an already-rotated token proves the session family is compromised,
// and every session for that user is terminated.
func TestRefreshDetectsReuse(t *testing.T) {
	h := newHarness(t)
	user := h.seedUser(t, "ops@example.com")

	login, _ := h.svc.Login(context.Background(), dto.LoginRequest{
		Email: "ops@example.com", Password: validPassword,
	}, SessionContext{})

	stolen := login.Tokens.RefreshToken

	// The legitimate client rotates first.
	if _, err := h.svc.Refresh(context.Background(),
		dto.RefreshRequest{RefreshToken: stolen}, SessionContext{}); err != nil {
		t.Fatalf("first Refresh() = %v", err)
	}

	if got := h.tokens.liveCount(user.ID, h.clock.Now()); got != 1 {
		t.Fatalf("live sessions after rotation = %d, want 1", got)
	}

	h.events.reset()

	// The attacker now presents the stolen (already-rotated) token.
	_, err := h.svc.Refresh(context.Background(),
		dto.RefreshRequest{RefreshToken: stolen}, SessionContext{IPAddress: "203.0.113.9"})
	if err == nil {
		t.Fatal("reusing a rotated token succeeded")
	}
	if code := apperror.From(err).Code; code != apperror.CodeUnauthorized {
		t.Errorf("code = %s, want UNAUTHORIZED", code)
	}

	// Every session is gone — including the legitimate client's new one.
	if got := h.tokens.liveCount(user.ID, h.clock.Now()); got != 0 {
		t.Errorf("live sessions after reuse detection = %d, want 0", got)
	}

	event, ok := h.events.find(entity.EventRefreshTokenRotated)
	if !ok {
		t.Fatal("no event published for reuse detection")
	}
	if event.Attributes["outcome"] != "reuse_detected" {
		t.Errorf("outcome = %v, want reuse_detected", event.Attributes["outcome"])
	}
}

func TestRefreshRejectsExpiredToken(t *testing.T) {
	h := newHarness(t)
	h.seedUser(t, "ops@example.com")

	login, _ := h.svc.Login(context.Background(), dto.LoginRequest{
		Email: "ops@example.com", Password: validPassword,
	}, SessionContext{})

	// Past the 7-day refresh TTL.
	h.clock.Advance(8 * 24 * time.Hour)

	_, err := h.svc.Refresh(context.Background(),
		dto.RefreshRequest{RefreshToken: login.Tokens.RefreshToken}, SessionContext{})
	if err == nil {
		t.Fatal("Refresh() = nil for an expired token")
	}
	if code := apperror.From(err).Code; code != apperror.CodeUnauthorized {
		t.Errorf("code = %s, want UNAUTHORIZED", code)
	}
}

func TestRefreshRejectsUnknownToken(t *testing.T) {
	h := newHarness(t)

	_, err := h.svc.Refresh(context.Background(),
		dto.RefreshRequest{RefreshToken: "completely-made-up-token-value-123456"}, SessionContext{})
	if err == nil {
		t.Fatal("Refresh() = nil for an unknown token")
	}
	if code := apperror.From(err).Code; code != apperror.CodeUnauthorized {
		t.Errorf("code = %s, want UNAUTHORIZED", code)
	}
}

// TestRefreshRejectsDeactivatedAccount: status is re-checked on every refresh,
// so an account locked mid-session stops minting access tokens immediately
// rather than at the end of the refresh token's week-long life.
func TestRefreshRejectsDeactivatedAccount(t *testing.T) {
	h := newHarness(t)
	user := h.seedUser(t, "ops@example.com")

	login, _ := h.svc.Login(context.Background(), dto.LoginRequest{
		Email: "ops@example.com", Password: validPassword,
	}, SessionContext{})

	// An administrator locks the account.
	locked := *user
	locked.Status = entity.StatusLocked
	if err := h.users.Update(context.Background(), &locked); err != nil {
		t.Fatalf("Update() = %v", err)
	}

	_, err := h.svc.Refresh(context.Background(),
		dto.RefreshRequest{RefreshToken: login.Tokens.RefreshToken}, SessionContext{})
	if err == nil {
		t.Fatal("Refresh() = nil for a locked account")
	}
	if code := apperror.From(err).Code; code != apperror.CodeForbidden {
		t.Errorf("code = %s, want FORBIDDEN", code)
	}
}

// ---------- Logout ----------

func TestLogoutRevokesSession(t *testing.T) {
	h := newHarness(t)
	user := h.seedUser(t, "ops@example.com")

	login, _ := h.svc.Login(context.Background(), dto.LoginRequest{
		Email: "ops@example.com", Password: validPassword,
	}, SessionContext{})

	if err := h.svc.Logout(context.Background(),
		dto.LogoutRequest{RefreshToken: login.Tokens.RefreshToken}); err != nil {
		t.Fatalf("Logout() = %v", err)
	}

	if got := h.tokens.liveCount(user.ID, h.clock.Now()); got != 0 {
		t.Errorf("live sessions = %d, want 0", got)
	}
	if !h.events.has(entity.EventUserLoggedOut) {
		t.Errorf("UserLoggedOut not published; got %v", h.events.names())
	}

	// The revoked token can no longer be refreshed.
	if _, err := h.svc.Refresh(context.Background(),
		dto.RefreshRequest{RefreshToken: login.Tokens.RefreshToken}, SessionContext{}); err == nil {
		t.Error("a logged-out token was still accepted for refresh")
	}
}

// TestLogoutIsIdempotent: a client retrying a logout must not receive an error,
// and an unknown token must not reveal whether it was ever valid.
func TestLogoutIsIdempotent(t *testing.T) {
	h := newHarness(t)
	h.seedUser(t, "ops@example.com")

	login, _ := h.svc.Login(context.Background(), dto.LoginRequest{
		Email: "ops@example.com", Password: validPassword,
	}, SessionContext{})

	for i := 0; i < 3; i++ {
		if err := h.svc.Logout(context.Background(),
			dto.LogoutRequest{RefreshToken: login.Tokens.RefreshToken}); err != nil {
			t.Fatalf("Logout() call %d = %v", i+1, err)
		}
	}

	if err := h.svc.Logout(context.Background(),
		dto.LogoutRequest{RefreshToken: "never-existed-token-value-0123456789"}); err != nil {
		t.Errorf("Logout() with an unknown token = %v, want nil", err)
	}
}

func TestLogoutAllSessions(t *testing.T) {
	h := newHarness(t)
	user := h.seedUser(t, "ops@example.com")

	var last string
	for i := 0; i < 3; i++ {
		login, err := h.svc.Login(context.Background(), dto.LoginRequest{
			Email: "ops@example.com", Password: validPassword,
		}, SessionContext{})
		if err != nil {
			t.Fatalf("Login() = %v", err)
		}
		last = login.Tokens.RefreshToken
	}

	if err := h.svc.Logout(context.Background(), dto.LogoutRequest{
		RefreshToken: last,
		AllSessions:  true,
	}); err != nil {
		t.Fatalf("Logout(all) = %v", err)
	}

	if got := h.tokens.liveCount(user.ID, h.clock.Now()); got != 0 {
		t.Errorf("live sessions = %d, want 0", got)
	}
}

// ---------- Me ----------

func TestMeReturnsProfileWithoutHash(t *testing.T) {
	h := newHarness(t)
	user := h.seedUser(t, "ops@example.com")

	got, err := h.svc.Me(authedContext(user))
	if err != nil {
		t.Fatalf("Me() = %v", err)
	}

	if got.ID != user.ID {
		t.Errorf("id = %s, want %s", got.ID, user.ID)
	}
	if got.Email != "ops@example.com" {
		t.Errorf("email = %q", got.Email)
	}
	// dto.UserResponse has no hash field by construction; this asserts the
	// mapper does not smuggle one into another field.
	if strings.Contains(got.FullName, "$2") {
		t.Error("a bcrypt hash leaked into the response")
	}
}

func TestMeRequiresAuthentication(t *testing.T) {
	h := newHarness(t)

	_, err := h.svc.Me(context.Background())
	if err == nil {
		t.Fatal("Me() = nil without an authenticated principal")
	}
	if code := apperror.From(err).Code; code != apperror.CodeUnauthorized {
		t.Errorf("code = %s, want UNAUTHORIZED", code)
	}
}

// ---------- audit events ----------

// TestEventsCarryNoSecrets: audit records are forwarded to systems with
// different access controls than the database, so they must carry identifiers
// and never credentials.
func TestEventsCarryNoSecrets(t *testing.T) {
	h := newHarness(t)

	got, err := h.svc.Register(context.Background(), dto.RegisterRequest{
		Email:    "ops@example.com",
		Password: validPassword,
		FullName: "Ops User",
	}, SessionContext{})
	if err != nil {
		t.Fatalf("Register() = %v", err)
	}

	for _, event := range h.events.events {
		for key, value := range event.Attributes {
			rendered := strings.ToLower(toString(value))

			if strings.Contains(rendered, strings.ToLower(validPassword)) {
				t.Errorf("event %s attribute %q contains the password", event.Name, key)
			}
			if rendered != "" && strings.Contains(rendered, got.Tokens.RefreshToken) {
				t.Errorf("event %s attribute %q contains the refresh token", event.Name, key)
			}
			if strings.Contains(key, "password") || strings.Contains(key, "token_hash") {
				t.Errorf("event %s has a sensitive attribute key %q", event.Name, key)
			}
		}
	}
}
