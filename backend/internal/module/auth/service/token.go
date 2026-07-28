package service

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/internal/config"
	"github.com/batokhehe/wms-saas/backend/internal/shared/port"
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// Token type claim values.
//
// Access and refresh tokens are structurally similar, and without an explicit
// type claim a refresh token would verify as an access token — handing an
// attacker who steals one a long-lived API credential. The claim is checked on
// every verification.
const (
	tokenTypeAccess = "access"
)

// refreshTokenBytes is the entropy of a raw refresh token.
//
// 32 bytes is 256 bits, matching the SHA-256 digest it is stored under. At that
// size a brute-force search is not merely impractical, it is thermodynamically
// impossible, which is why the token is stored under a fast hash rather than a
// slow KDF.
const refreshTokenBytes = 32

// Claims is the access token payload.
type Claims struct {
	jwt.RegisteredClaims

	// TokenType distinguishes access from refresh. See the constants above.
	TokenType string `json:"typ"`

	// Nothing else is included. In particular there is no email, no name and no
	// role: a JWT is signed but not encrypted, so every claim is readable by
	// anyone holding the token. Personal data in a token is personal data in
	// the client's local storage, in proxy logs and in browser history.
	//
	// CompanyID is absent by design — see Authentication.md. Identity does not
	// depend on company, and Sprint 2 adds tenancy without reissuing tokens.
}

// TokenService issues and verifies tokens.
type TokenService struct {
	cfg   config.JWTConfig
	clock port.Clock
	ids   port.IDGenerator
}

// NewTokenService builds the token service.
func NewTokenService(cfg config.JWTConfig, clock port.Clock, ids port.IDGenerator) *TokenService {
	return &TokenService{cfg: cfg, clock: clock, ids: ids}
}

// AccessTokenTTL exposes the configured access token lifetime, so the handler
// can report expires_in without reaching into config itself.
func (s *TokenService) AccessTokenTTL() time.Duration { return s.cfg.AccessTokenTTL }

// RefreshTokenTTL exposes the configured refresh token lifetime.
func (s *TokenService) RefreshTokenTTL() time.Duration { return s.cfg.RefreshTokenTTL }

// IssueAccessToken mints a signed access token for a user.
//
// HS256 (symmetric) rather than RS256: there is one issuer and one verifier,
// both in this process, so asymmetric signing would add key distribution
// complexity for no benefit. If a separate service ever needs to verify tokens
// without the ability to mint them, that is the point to move to RS256 — and it
// is a change confined to this file.
func (s *TokenService) IssueAccessToken(userID uuid.UUID) (string, time.Time, error) {
	now := s.clock.Now()
	expiresAt := now.Add(s.cfg.AccessTokenTTL)

	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID.String(),
			Issuer:    s.cfg.Issuer,
			Audience:  jwt.ClaimStrings{s.cfg.Audience},
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			// A unique id per token, so a future deny-list can revoke one
			// specific access token without invalidating every session.
			ID: s.ids.NewID().String(),
		},
		TokenType: tokenTypeAccess,
	}

	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).
		SignedString([]byte(s.cfg.Secret))
	if err != nil {
		return "", time.Time{}, apperror.Internal("Could not issue an access token").
			WithOp("auth.token.IssueAccessToken").
			WithCause(err)
	}

	return signed, expiresAt, nil
}

// VerifyAccessToken validates a token and returns its claims.
//
// Every failure produces the same generic UNAUTHORIZED message. Distinguishing
// "expired" from "bad signature" from "wrong audience" tells an attacker
// probing the endpoint exactly which part of a forged token to fix next.
func (s *TokenService) VerifyAccessToken(raw string) (*Claims, error) {
	unauthorized := func(cause error) error {
		return apperror.Unauthorized("The access token is invalid or has expired").
			WithOp("auth.token.VerifyAccessToken").
			WithCause(cause)
	}

	claims := &Claims{}

	parsed, err := jwt.ParseWithClaims(raw, claims,
		func(token *jwt.Token) (any, error) {
			// Pinning the algorithm is the defence against the classic JWT
			// attack: an attacker re-signs a forged token with "alg":"none" or
			// swaps HS256 for RS256 so the public key is treated as an HMAC
			// secret. Accepting whatever the token header claims is what makes
			// that work.
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method %q", token.Header["alg"])
			}
			return []byte(s.cfg.Secret), nil
		},
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(s.cfg.Issuer),
		jwt.WithAudience(s.cfg.Audience),
		jwt.WithLeeway(s.cfg.ClockSkew),
		// Require exp: a token without one never expires, and a bug that omits
		// it would issue permanent credentials silently.
		jwt.WithExpirationRequired(),
		// Validate exp and nbf against the INJECTED clock, not the wall clock.
		// Without this the library reads time.Now() directly, so issuance would
		// be testable but verification would not — and a test advancing the
		// fake clock past expiry would still see the token accepted.
		jwt.WithTimeFunc(s.clock.Now),
	)
	if err != nil {
		return nil, unauthorized(err)
	}
	if !parsed.Valid {
		return nil, unauthorized(errors.New("token reported invalid"))
	}

	// A refresh token presented as an access token must be rejected, or
	// stealing one would yield an API credential rather than merely a session.
	if claims.TokenType != tokenTypeAccess {
		return nil, unauthorized(fmt.Errorf("token type %q is not an access token", claims.TokenType))
	}

	if _, err := uuid.Parse(claims.Subject); err != nil {
		return nil, unauthorized(fmt.Errorf("subject %q is not a valid uuid", claims.Subject))
	}

	return claims, nil
}

// UserID extracts the subject as a UUID. Safe to call on claims returned by
// VerifyAccessToken, which has already validated the format.
func (c *Claims) UserID() (uuid.UUID, error) {
	return uuid.Parse(c.Subject)
}

// GenerateRefreshToken produces a raw token and its storage hash.
//
// The raw value is returned to the caller exactly once — it goes into the HTTP
// response and is never persisted, logged or recoverable. Only the digest is
// stored, so a database leak yields nothing usable.
func (s *TokenService) GenerateRefreshToken() (raw, hash string, err error) {
	buf := make([]byte, refreshTokenBytes)

	// crypto/rand, never math/rand. A predictable refresh token is a session
	// anyone can guess; math/rand seeded from the clock has been the root cause
	// of exactly this class of breach.
	if _, err := rand.Read(buf); err != nil {
		return "", "", apperror.Internal("Could not issue a refresh token").
			WithOp("auth.token.GenerateRefreshToken").
			WithCause(err)
	}

	// URL-safe and unpadded, so the token survives being placed in a header,
	// a query string or a JSON body without escaping.
	raw = base64.RawURLEncoding.EncodeToString(buf)

	return raw, HashRefreshToken(raw), nil
}

// HashRefreshToken derives the storage digest of a raw refresh token.
//
// SHA-256 rather than bcrypt, which is the opposite of the choice made for
// passwords — deliberately:
//
//   - bcrypt's slowness defends LOW-entropy secrets. This token carries 256
//     bits of entropy, so there is no guessing attack to slow down.
//   - The refresh path must look the token up BY its hash. bcrypt embeds a
//     random per-hash salt, so the digest is not reproducible from the input;
//     finding a match would mean scanning every row and bcrypt-comparing each.
//     SHA-256 is deterministic, so the lookup is one indexed equality.
//
// Lowercase hex, so the stored value is always exactly 64 characters and
// matches the CHAR(64) column.
func HashRefreshToken(raw string) string {
	digest := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(digest[:])
}
