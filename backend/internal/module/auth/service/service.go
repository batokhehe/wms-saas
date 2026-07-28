// Package service holds the auth module's business rules.
//
// LAYER RULE: no gin, no gorm, no http. Every use case takes context.Context
// and returns DTOs and typed errors, so the same logic is reachable from an
// HTTP handler, a CLI command or a unit test with no infrastructure.
package service

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/batokhehe/wms-saas/backend/internal/module/auth/dto"
	"github.com/batokhehe/wms-saas/backend/internal/module/auth/entity"
	"github.com/batokhehe/wms-saas/backend/internal/module/auth/mapper"
	"github.com/batokhehe/wms-saas/backend/internal/module/auth/repository"
	"github.com/batokhehe/wms-saas/backend/internal/module/auth/validator"
	"github.com/batokhehe/wms-saas/backend/internal/shared/appcontext"
	"github.com/batokhehe/wms-saas/backend/internal/shared/port"
	"github.com/batokhehe/wms-saas/backend/internal/shared/transaction"
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// dummyHash is a real bcrypt hash of an unguessable value, compared against
// when no account matches the submitted email.
//
// Without it, a request for a non-existent address returns in microseconds
// while a request for a real one takes the ~250ms bcrypt costs. That gap is
// trivially measurable over the network and turns the login endpoint into an
// account enumeration oracle: an attacker learns which addresses are registered
// and can target password-spraying at them.
//
// Generated at cost 12 from a random 32-byte value; the plaintext is unknown
// and irrelevant, because this comparison must always fail.
const dummyHash = "$2a$12$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy"

// Service implements the authentication use cases.
//
// It depends on repository interfaces and on ports, never on concrete
// infrastructure — no *gorm.DB, no *redis.Client, no time.Now().
type Service struct {
	users     repository.UserRepository
	tokens    repository.RefreshTokenRepository
	hasher    PasswordHasher
	tokenSvc  *TokenService
	clock     port.Clock
	tx        transaction.Manager
	events    EventPublisher
	maxActive int
}

// New builds the service.
func New(
	users repository.UserRepository,
	tokens repository.RefreshTokenRepository,
	hasher PasswordHasher,
	tokenSvc *TokenService,
	clock port.Clock,
	tx transaction.Manager,
	events EventPublisher,
	maxSessionsPerUser int,
) *Service {
	return &Service{
		users:     users,
		tokens:    tokens,
		hasher:    hasher,
		tokenSvc:  tokenSvc,
		clock:     clock,
		tx:        tx,
		events:    events,
		maxActive: maxSessionsPerUser,
	}
}

// SessionContext is the request provenance recorded against a session.
//
// It is assembled by the handler from the HTTP request and passed explicitly,
// so the service never touches *gin.Context.
type SessionContext struct {
	Device    string
	IPAddress string
	UserAgent string
}

// ---------- Register ----------

// Register creates an account and returns a token pair.
//
// Flow: validate → hash → create user → issue access token → issue refresh
// token → return both.
//
// The whole thing runs in one transaction. Without it, a failure between
// creating the user and storing the refresh token would leave an account whose
// caller believes registration failed — and whose email is now taken, so the
// retry fails with a conflict the user cannot resolve.
func (s *Service) Register(
	ctx context.Context,
	req dto.RegisterRequest,
	session SessionContext,
) (dto.AuthResponse, error) {
	if err := validator.ValidateRegister(req); err != nil {
		return dto.AuthResponse{}, err
	}

	hash, err := s.hasher.Hash(req.Password)
	if err != nil {
		return dto.AuthResponse{}, err
	}

	var response dto.AuthResponse

	err = s.tx.RunInTransaction(ctx, func(ctx context.Context) error {
		email := entity.NormalizeEmail(req.Email)

		// Checked explicitly so the common case produces a clear message. The
		// unique index is still the real guarantee — two simultaneous
		// registrations both pass this check, and the second one's INSERT is
		// what actually fails, translated to CONFLICT by the repository.
		exists, err := s.users.ExistsByEmail(ctx, email)
		if err != nil {
			return err
		}
		if exists {
			return apperror.Conflict("An account with this email already exists").
				WithOp("auth.service.Register")
		}

		user := mapper.FromRegisterRequest(req, hash)
		if err := s.users.Create(ctx, &user); err != nil {
			return err
		}

		tokens, err := s.issueTokenPair(ctx, &user, session)
		if err != nil {
			return err
		}

		response = dto.AuthResponse{
			User:   mapper.ToUserResponse(&user),
			Tokens: tokens,
		}

		s.publish(ctx, entity.EventUserRegistered, user.ID, nil)
		return nil
	})
	if err != nil {
		return dto.AuthResponse{}, err
	}

	return response, nil
}

// ---------- Login ----------

// Login authenticates and returns a fresh token pair.
//
// Flow: find user → verify password → check status → issue new access token →
// issue new refresh token → record login.
//
// Note what login does NOT do: revoke the caller's other refresh tokens. A
// warehouse operator legitimately holds sessions on a handheld scanner, a
// desktop and a phone at once, and logging in on one must not sign them out of
// the others. Old sessions end on logout, on rotation, on expiry, or when the
// concurrent-session cap evicts the oldest.
func (s *Service) Login(
	ctx context.Context,
	req dto.LoginRequest,
	session SessionContext,
) (dto.AuthResponse, error) {
	user, err := s.authenticate(ctx, req.Email, req.Password)
	if err != nil {
		return dto.AuthResponse{}, err
	}

	session.Device = req.Device

	var response dto.AuthResponse

	err = s.tx.RunInTransaction(ctx, func(ctx context.Context) error {
		tokens, err := s.issueTokenPair(ctx, user, session)
		if err != nil {
			return err
		}

		now := s.clock.Now()
		if err := s.users.UpdateLastLogin(ctx, user.ID, now); err != nil {
			return err
		}
		user.RecordLogin(now)

		response = dto.AuthResponse{
			User:   mapper.ToUserResponse(user),
			Tokens: tokens,
		}

		s.publish(ctx, entity.EventUserLoggedIn, user.ID, map[string]any{
			"ip_address": session.IPAddress,
			"device":     session.Device,
		})
		return nil
	})
	if err != nil {
		return dto.AuthResponse{}, err
	}

	return response, nil
}

// authenticate resolves credentials to a user, or fails.
//
// Every failure — unknown email, wrong password, locked account — returns the
// SAME error. Distinguishing them tells an attacker which half of a credential
// pair was correct, which is the difference between guessing a password and
// confirming an account exists.
func (s *Service) authenticate(ctx context.Context, email, password string) (*entity.User, error) {
	invalid := apperror.Unauthorized("Invalid email or password").
		WithOp("auth.service.authenticate")

	user, err := s.users.FindByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, apperror.ErrNotFound) {
			// Burn the same time a real verification would, so response latency
			// does not reveal whether the account exists.
			s.hasher.Verify(dummyHash, password)
			return nil, invalid
		}
		return nil, err
	}

	if !s.hasher.Verify(user.PasswordHash, password) {
		return nil, invalid
	}

	// Status is checked AFTER the password, deliberately. Checking first would
	// let anyone discover which accounts are locked without knowing a password.
	if !user.CanAuthenticate() {
		if user.IsLocked() {
			// A locked account is a distinct, actionable message — but only to
			// someone who has already proven they know the password, so it
			// reveals nothing to an attacker.
			return nil, apperror.Forbidden(
				"This account is locked. Contact your administrator.").
				WithOp("auth.service.authenticate")
		}
		return nil, apperror.Forbidden("This account is not active").
			WithOp("auth.service.authenticate")
	}

	return user, nil
}

// ---------- Refresh ----------

// Refresh rotates a refresh token and issues a new access token.
//
// Flow: hash the presented token → look it up → detect reuse → validate →
// revoke the old → issue a new pair.
//
// Rotation means a refresh token is single-use. That is what makes theft
// detectable: the legitimate client and the attacker cannot both use the same
// token, so the second presentation is proof that one of them is not the owner.
func (s *Service) Refresh(
	ctx context.Context,
	req dto.RefreshRequest,
	session SessionContext,
) (dto.AuthResponse, error) {
	invalid := apperror.Unauthorized("The refresh token is invalid or has expired").
		WithOp("auth.service.Refresh")

	// The raw token is hashed immediately and the raw value is never used
	// again, so it cannot reach a log line or an error message.
	tokenHash := HashRefreshToken(req.RefreshToken)

	stored, err := s.tokens.FindByHash(ctx, tokenHash)
	if err != nil {
		if errors.Is(err, apperror.ErrNotFound) {
			return dto.AuthResponse{}, invalid
		}
		return dto.AuthResponse{}, err
	}

	now := s.clock.Now()

	// Reuse detection. A revoked token being presented means it was already
	// rotated — so either an attacker stole it and the real client rotated
	// first, or the client leaked it. Either way the session family is
	// compromised and every session for the user is terminated.
	//
	// This is the single most valuable property of rotation. Without it, a
	// stolen refresh token grants an attacker indefinite access, silently, for
	// as long as they keep rotating it.
	if stored.IsRevoked() {
		return dto.AuthResponse{}, s.handleTokenReuse(ctx, stored.UserID, session, invalid)
	}

	if stored.IsExpired(now) {
		return dto.AuthResponse{}, invalid
	}

	user, err := s.users.FindByID(ctx, stored.UserID)
	if err != nil {
		if errors.Is(err, apperror.ErrNotFound) {
			return dto.AuthResponse{}, invalid
		}
		return dto.AuthResponse{}, err
	}

	// Re-checked on every refresh, not just at login. An account locked five
	// minutes ago must not keep minting access tokens for the rest of the
	// refresh token's week-long life.
	if !user.CanAuthenticate() {
		return dto.AuthResponse{}, apperror.Forbidden("This account is no longer active").
			WithOp("auth.service.Refresh")
	}

	var response dto.AuthResponse

	err = s.tx.RunInTransaction(ctx, func(ctx context.Context) error {
		// Revoke before issuing, and conditionally. RevokeIfLive is an atomic
		// UPDATE ... WHERE revoked_at IS NULL, so if a concurrent request
		// rotated this token first, this call reports false rather than both
		// requests minting a session from one token.
		rotated, err := s.tokens.RevokeIfLive(ctx, stored.ID, now)
		if err != nil {
			return err
		}
		if !rotated {
			return errTokenAlreadyRotated
		}

		tokens, err := s.issueTokenPair(ctx, user, session)
		if err != nil {
			return err
		}

		response = dto.AuthResponse{
			User:   mapper.ToUserResponse(user),
			Tokens: tokens,
		}
		return nil
	})

	// A lost race is indistinguishable from theft: one token, two consumers.
	// Handled after the transaction has rolled back, so the mass revocation
	// runs in its own transaction and actually commits.
	if errors.Is(err, errTokenAlreadyRotated) {
		return dto.AuthResponse{}, s.handleTokenReuse(ctx, stored.UserID, session, invalid)
	}
	if err != nil {
		return dto.AuthResponse{}, err
	}

	s.publish(ctx, entity.EventRefreshTokenRotated, user.ID, map[string]any{
		"outcome": "rotated",
	})

	return response, nil
}

// errTokenAlreadyRotated signals that another request consumed the token first.
// It never reaches a client; Refresh converts it into the generic auth failure.
var errTokenAlreadyRotated = errors.New("auth: refresh token already rotated")

// handleTokenReuse terminates every session for a user after detecting that a
// refresh token was presented twice.
//
// The revocation runs in its OWN transaction, deliberately. The request it
// belongs to is about to fail, and doing this inside that request's transaction
// would roll the revocation back at the moment it matters most — leaving the
// attacker's stolen session alive. This was a real defect caught in end-to-end
// testing, not a hypothetical.
//
// It always returns authErr: the caller must not learn that reuse was detected.
func (s *Service) handleTokenReuse(
	ctx context.Context,
	userID uuid.UUID,
	session SessionContext,
	authErr error,
) error {
	now := s.clock.Now()

	var revoked int64

	if err := s.tx.RunInTransaction(ctx, func(ctx context.Context) error {
		count, err := s.tokens.RevokeAllForUser(ctx, userID, now)
		if err != nil {
			return err
		}
		revoked = count
		return nil
	}); err != nil {
		// The revocation failed. Report the infrastructure error rather than a
		// clean 401, because silently continuing would leave the compromised
		// sessions live with nothing recording that we tried.
		return err
	}

	// Logged at warn: this is a security signal an operator should see. The
	// token itself is never logged — only the user it belonged to.
	appcontext.Logger(ctx).Warn("refresh token reuse detected; all sessions revoked",
		zap.String("user_id", userID.String()),
		zap.Int64("sessions_revoked", revoked),
	)

	s.publish(ctx, entity.EventRefreshTokenRotated, userID, map[string]any{
		"outcome":          "reuse_detected",
		"sessions_revoked": revoked,
		"ip_address":       session.IPAddress,
	})

	return authErr
}

// ---------- Logout ----------

// Logout revokes a refresh token, ending the session.
//
// It is idempotent and never reports failure for an unknown or already-revoked
// token. A logout that returns an error leaves the client unsure whether it is
// signed out, and the usual response is to retry — while a 404 here would also
// confirm to an attacker which stolen tokens are still live.
//
// The access token remains valid until it expires. That is inherent to
// stateless tokens and is why the access TTL is short; see Security.md.
func (s *Service) Logout(ctx context.Context, req dto.LogoutRequest) error {
	tokenHash := HashRefreshToken(req.RefreshToken)

	return s.tx.RunInTransaction(ctx, func(ctx context.Context) error {
		stored, err := s.tokens.FindByHash(ctx, tokenHash)
		if err != nil {
			if errors.Is(err, apperror.ErrNotFound) {
				// Nothing to revoke. Reported as success, per the note above.
				return nil
			}
			return err
		}

		now := s.clock.Now()

		if req.AllSessions {
			revoked, err := s.tokens.RevokeAllForUser(ctx, stored.UserID, now)
			if err != nil {
				return err
			}

			s.publish(ctx, entity.EventUserLoggedOut, stored.UserID, map[string]any{
				"scope":            "all_sessions",
				"sessions_revoked": revoked,
			})
			return nil
		}

		if err := s.tokens.Revoke(ctx, stored.ID, now); err != nil {
			return err
		}

		s.publish(ctx, entity.EventUserLoggedOut, stored.UserID, map[string]any{
			"scope": "single_session",
		})
		return nil
	})
}

// ---------- Me ----------

// Me returns the authenticated user's profile.
//
// The user id comes from the RequestContext, populated by the JWT middleware —
// never from a request field. A client cannot ask for someone else's profile
// because it has no way to name one.
func (s *Service) Me(ctx context.Context) (dto.UserResponse, error) {
	rc := appcontext.From(ctx)

	if !rc.IsAuthenticated() {
		return dto.UserResponse{}, apperror.Unauthorized("Authentication is required").
			WithOp("auth.service.Me")
	}

	user, err := s.users.FindByID(ctx, *rc.UserID)
	if err != nil {
		return dto.UserResponse{}, err
	}

	return mapper.ToUserResponse(user), nil
}

// ---------- internals ----------

// issueTokenPair mints an access token and a stored refresh token.
//
// Called inside a transaction by every flow that produces tokens, so the
// session row and whatever else the flow writes commit together.
func (s *Service) issueTokenPair(
	ctx context.Context,
	user *entity.User,
	session SessionContext,
) (dto.TokenPair, error) {
	accessToken, accessExpiresAt, err := s.tokenSvc.IssueAccessToken(user.ID)
	if err != nil {
		return dto.TokenPair{}, err
	}

	rawRefresh, refreshHash, err := s.tokenSvc.GenerateRefreshToken()
	if err != nil {
		return dto.TokenPair{}, err
	}

	now := s.clock.Now()
	refreshExpiresAt := now.Add(s.tokenSvc.RefreshTokenTTL())

	if err := s.enforceSessionLimit(ctx, user.ID, now); err != nil {
		return dto.TokenPair{}, err
	}

	record := entity.RefreshToken{
		UserID:    user.ID,
		TokenHash: refreshHash,
		ExpiresAt: refreshExpiresAt,
		Device:    truncate(session.Device, 255),
		UserAgent: truncate(session.UserAgent, 512),
	}
	if session.IPAddress != "" {
		ip := session.IPAddress
		record.IPAddress = &ip
	}

	if err := s.tokens.Create(ctx, &record); err != nil {
		return dto.TokenPair{}, err
	}

	return dto.TokenPair{
		AccessToken:  accessToken,
		RefreshToken: rawRefresh,
		TokenType:    "Bearer",
		// Seconds, so a client can schedule a refresh without depending on its
		// own clock being correct.
		ExpiresIn:        int64(time.Until(accessExpiresAt).Seconds()),
		RefreshExpiresAt: refreshExpiresAt,
	}, nil
}

// enforceSessionLimit evicts the oldest session when a user is at the cap.
//
// Evicting rather than rejecting: refusing the login of someone who legitimately
// has ten devices would be a support ticket, while silently retiring their
// oldest session is what every consumer product does and what users expect.
func (s *Service) enforceSessionLimit(ctx context.Context, userID uuid.UUID, now time.Time) error {
	if s.maxActive <= 0 {
		return nil
	}

	live, err := s.tokens.CountLiveForUser(ctx, userID, now)
	if err != nil {
		return err
	}
	if live < int64(s.maxActive) {
		return nil
	}

	oldest, err := s.tokens.OldestLiveForUser(ctx, userID, now)
	if err != nil {
		if errors.Is(err, apperror.ErrNotFound) {
			return nil
		}
		return err
	}

	return s.tokens.Revoke(ctx, oldest.ID, now)
}

// publish emits a domain event, tagged with the current request id.
func (s *Service) publish(
	ctx context.Context,
	name entity.EventName,
	userID uuid.UUID,
	attributes map[string]any,
) {
	event := entity.NewEvent(name, userID, s.clock.Now(), appcontext.RequestID(ctx))
	for key, value := range attributes {
		event = event.With(key, value)
	}

	s.events.Publish(ctx, event)
}

// truncate bounds a client-supplied string to its column width.
//
// The value is untrusted, and a 10 KB User-Agent would otherwise fail the
// INSERT and turn a cosmetic detail into a failed login.
func truncate(value string, max int) string {
	if len(value) <= max {
		return value
	}
	return value[:max]
}
