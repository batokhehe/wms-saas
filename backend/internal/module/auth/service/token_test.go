package service

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/internal/config"
	adapterclock "github.com/batokhehe/wms-saas/backend/internal/shared/adapter/clock"
	adapterid "github.com/batokhehe/wms-saas/backend/internal/shared/adapter/id"
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

const testSecret = "test-secret-that-is-at-least-32-characters-long"

// fakeNow is the instant every fake clock in this file is pinned to.
//
// Hand-built forged tokens must set exp relative to THIS, not to time.Now():
// verification runs on the injected clock, so a token expiring an hour after
// the real wall clock is already expired from the verifier's point of view —
// and the test would pass for the wrong reason.
var fakeNow = time.Date(2026, 7, 22, 10, 0, 0, 0, time.UTC)

func testJWTConfig() config.JWTConfig {
	return config.JWTConfig{
		Secret:          testSecret,
		Issuer:          "wms-saas",
		Audience:        "wms-saas-api",
		AccessTokenTTL:  15 * time.Minute,
		RefreshTokenTTL: 7 * 24 * time.Hour,
		ClockSkew:       0,
	}
}

func newTokenService(t *testing.T) (*TokenService, *adapterclock.Fake) {
	t.Helper()
	clock := adapterclock.NewFakeAt("2026-07-22T10:00:00Z")
	return NewTokenService(testJWTConfig(), clock, adapterid.NewSequential()), clock
}

func TestIssueAndVerifyAccessToken(t *testing.T) {
	svc, _ := newTokenService(t)
	userID := uuid.New()

	token, expiresAt, err := svc.IssueAccessToken(userID)
	if err != nil {
		t.Fatalf("IssueAccessToken() = %v", err)
	}

	if want := "2026-07-22T10:15:00Z"; expiresAt.UTC().Format(time.RFC3339) != want {
		t.Errorf("expiresAt = %s, want %s", expiresAt.UTC().Format(time.RFC3339), want)
	}

	claims, err := svc.VerifyAccessToken(token)
	if err != nil {
		t.Fatalf("VerifyAccessToken() = %v", err)
	}

	got, err := claims.UserID()
	if err != nil {
		t.Fatalf("UserID() = %v", err)
	}
	if got != userID {
		t.Errorf("subject = %s, want %s", got, userID)
	}
}

// TestTokenCarriesNoPersonalData is a privacy control. A JWT is signed but NOT
// encrypted: every claim is readable by anyone holding the token, and the token
// lives in client storage, proxy logs and browser history.
func TestTokenCarriesNoPersonalData(t *testing.T) {
	svc, _ := newTokenService(t)

	token, _, err := svc.IssueAccessToken(uuid.New())
	if err != nil {
		t.Fatalf("IssueAccessToken() = %v", err)
	}

	// The payload is base64url; decode it and check nothing sensitive is there.
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("token has %d segments, want 3", len(parts))
	}

	payload, err := jwt.NewParser().DecodeSegment(parts[1])
	if err != nil {
		t.Fatalf("decoding payload: %v", err)
	}

	for _, forbidden := range []string{"email", "password", "full_name", "company"} {
		if strings.Contains(strings.ToLower(string(payload)), forbidden) {
			t.Errorf("token payload contains %q: %s", forbidden, payload)
		}
	}
}

// TestVerifyRejectsExpiredToken exercises the fake clock: the token is issued,
// time is advanced past its TTL, and verification must fail.
func TestVerifyRejectsExpiredToken(t *testing.T) {
	svc, clock := newTokenService(t)

	token, _, _ := svc.IssueAccessToken(uuid.New())

	clock.Advance(16 * time.Minute)

	if _, err := svc.VerifyAccessToken(token); err == nil {
		t.Fatal("VerifyAccessToken() = nil for an expired token")
	}
}

// TestVerifyHonoursClockSkew checks a token just past expiry is still accepted
// within the configured leeway, so a few seconds of drift between hosts does
// not sign users out.
func TestVerifyHonoursClockSkew(t *testing.T) {
	cfg := testJWTConfig()
	cfg.ClockSkew = 60 * time.Second

	clock := adapterclock.NewFakeAt("2026-07-22T10:00:00Z")
	svc := NewTokenService(cfg, clock, adapterid.NewSequential())

	token, _, _ := svc.IssueAccessToken(uuid.New())

	clock.Advance(15*time.Minute + 30*time.Second)

	if _, err := svc.VerifyAccessToken(token); err != nil {
		t.Errorf("VerifyAccessToken() = %v within the skew allowance", err)
	}
}

// TestVerifyRejectsWrongSecret is the core forgery defence.
func TestVerifyRejectsWrongSecret(t *testing.T) {
	svc, _ := newTokenService(t)
	token, _, _ := svc.IssueAccessToken(uuid.New())

	other := NewTokenService(config.JWTConfig{
		Secret:         "a-completely-different-secret-32-chars-plus",
		Issuer:         "wms-saas",
		Audience:       "wms-saas-api",
		AccessTokenTTL: 15 * time.Minute,
	}, adapterclock.NewFakeAt("2026-07-22T10:00:00Z"), adapterid.NewSequential())

	if _, err := other.VerifyAccessToken(token); err == nil {
		t.Fatal("a token signed with a different secret verified")
	}
}

// TestVerifyRejectsAlgNone is the classic JWT attack: strip the signature and
// claim the algorithm is "none". Accepting whatever the header declares is what
// makes it work, which is why the keyfunc pins HMAC.
func TestVerifyRejectsAlgNone(t *testing.T) {
	svc, _ := newTokenService(t)

	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   uuid.New().String(),
			Issuer:    "wms-saas",
			Audience:  jwt.ClaimStrings{"wms-saas-api"},
			ExpiresAt: jwt.NewNumericDate(fakeNow.Add(time.Hour)),
		},
		TokenType: tokenTypeAccess,
	}

	forged, err := jwt.NewWithClaims(jwt.SigningMethodNone, claims).
		SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("building the forged token: %v", err)
	}

	if _, err := svc.VerifyAccessToken(forged); err == nil {
		t.Fatal("an alg=none token verified")
	}
}

// TestVerifyRejectsWrongIssuerAndAudience stops a token minted by a sibling
// service that shares the secret from being accepted here.
func TestVerifyRejectsWrongIssuerAndAudience(t *testing.T) {
	clock := adapterclock.NewFakeAt("2026-07-22T10:00:00Z")

	foreign := NewTokenService(config.JWTConfig{
		Secret:         testSecret, // same secret
		Issuer:         "some-other-service",
		Audience:       "some-other-api",
		AccessTokenTTL: 15 * time.Minute,
	}, clock, adapterid.NewSequential())

	token, _, _ := foreign.IssueAccessToken(uuid.New())

	svc := NewTokenService(testJWTConfig(), clock, adapterid.NewSequential())
	if _, err := svc.VerifyAccessToken(token); err == nil {
		t.Fatal("a token with a foreign issuer and audience verified")
	}
}

// TestVerifyRejectsNonAccessTokenType stops a refresh-typed token being used as
// an API credential.
func TestVerifyRejectsNonAccessTokenType(t *testing.T) {
	svc, _ := newTokenService(t)

	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   uuid.New().String(),
			Issuer:    "wms-saas",
			Audience:  jwt.ClaimStrings{"wms-saas-api"},
			ExpiresAt: jwt.NewNumericDate(fakeNow.Add(time.Hour)),
		},
		TokenType: "refresh",
	}

	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(testSecret))
	if err != nil {
		t.Fatalf("signing: %v", err)
	}

	if _, err := svc.VerifyAccessToken(token); err == nil {
		t.Fatal("a refresh-typed token verified as an access token")
	}
}

// TestVerifyErrorsAreGeneric checks that no failure mode tells an attacker
// which part of a forged token to fix next.
func TestVerifyErrorsAreGeneric(t *testing.T) {
	svc, clock := newTokenService(t)

	expired, _, _ := svc.IssueAccessToken(uuid.New())
	clock.Advance(time.Hour)

	for name, token := range map[string]string{
		"expired":   expired,
		"garbage":   "not.a.token",
		"empty":     "",
		"truncated": strings.Split(expired, ".")[0],
	} {
		_, err := svc.VerifyAccessToken(token)
		if err == nil {
			t.Errorf("%s: verified unexpectedly", name)
			continue
		}

		appErr := apperror.From(err)
		if appErr.Code != apperror.CodeUnauthorized {
			t.Errorf("%s: code = %s, want UNAUTHORIZED", name, appErr.Code)
		}
		if appErr.Message != "The access token is invalid or has expired" {
			t.Errorf("%s: message = %q, want the generic message", name, appErr.Message)
		}
	}
}

// ---------- refresh tokens ----------

func TestGenerateRefreshTokenIsRandom(t *testing.T) {
	svc, _ := newTokenService(t)

	seen := make(map[string]struct{}, 500)
	for i := 0; i < 500; i++ {
		raw, hash, err := svc.GenerateRefreshToken()
		if err != nil {
			t.Fatalf("GenerateRefreshToken() = %v", err)
		}

		if _, duplicate := seen[raw]; duplicate {
			t.Fatal("GenerateRefreshToken() produced a duplicate")
		}
		seen[raw] = struct{}{}

		if hash == raw {
			t.Fatal("the stored hash equals the raw token")
		}
	}
}

// TestRefreshTokenHashIsDeterministic is what makes lookup-by-hash possible —
// and is precisely why SHA-256 is used here rather than bcrypt, whose per-hash
// salt would make the digest unreproducible.
func TestRefreshTokenHashIsDeterministic(t *testing.T) {
	svc, _ := newTokenService(t)

	raw, hash, _ := svc.GenerateRefreshToken()

	if again := HashRefreshToken(raw); again != hash {
		t.Errorf("HashRefreshToken() = %s, want %s", again, hash)
	}
	if HashRefreshToken(raw+"x") == hash {
		t.Error("a different token produced the same hash")
	}
}

// TestRefreshTokenHashShape pins the storage format against the CHAR(64)
// column: a mismatch would fail every insert at runtime.
func TestRefreshTokenHashShape(t *testing.T) {
	svc, _ := newTokenService(t)

	raw, hash, _ := svc.GenerateRefreshToken()

	if len(hash) != 64 {
		t.Errorf("hash length = %d, want 64 (hex sha-256)", len(hash))
	}
	if strings.ToLower(hash) != hash {
		t.Error("hash is not lowercase hex")
	}
	// 32 random bytes in unpadded base64url.
	if len(raw) != 43 {
		t.Errorf("raw token length = %d, want 43", len(raw))
	}
	if strings.ContainsAny(raw, "+/=") {
		t.Errorf("raw token %q is not URL-safe unpadded base64", raw)
	}
}

func TestVerifyAccessTokenRejectsMalformedSubject(t *testing.T) {
	svc, _ := newTokenService(t)

	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "not-a-uuid",
			Issuer:    "wms-saas",
			Audience:  jwt.ClaimStrings{"wms-saas-api"},
			ExpiresAt: jwt.NewNumericDate(fakeNow.Add(time.Hour)),
		},
		TokenType: tokenTypeAccess,
	}

	token, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(testSecret))

	_, err := svc.VerifyAccessToken(token)
	if err == nil {
		t.Fatal("a token with a non-UUID subject verified")
	}
	var appErr *apperror.Error
	if !errors.As(err, &appErr) || appErr.Code != apperror.CodeUnauthorized {
		t.Errorf("err = %v, want UNAUTHORIZED", err)
	}
}
