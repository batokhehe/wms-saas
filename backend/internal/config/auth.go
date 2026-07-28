package config

import (
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// Weak secrets that appear in tutorials, .env.example files and copy-pasted
// deployment manifests. Rejecting them by name is crude, but the failure it
// prevents — a production deployment signing tokens with "secret" — is one that
// no amount of documentation reliably stops.
var forbiddenSecrets = map[string]struct{}{
	"secret":        {},
	"supersecret":   {},
	"changeme":      {},
	"change-me":     {},
	"jwt-secret":    {},
	"your-secret":   {},
	"mysecret":      {},
	"dev-secret":    {},
	"test":          {},
	"password":      {},
	"insecure":      {},
	"wms-saas-dev":  {},
	"secretkeyhere": {},
}

// MinSecretLength is the shortest accepted signing secret.
//
// HS256 uses HMAC-SHA256, whose security ceiling is a 256-bit key. A shorter
// secret is brute-forceable offline: an attacker who captures one token can
// grind candidate secrets locally at billions per second, and on success can
// mint a token for any user in the system.
const MinSecretLength = 32

// validate enforces the rules that make auth configuration safe.
//
// These run at boot, so a bad value fails the process rather than surfacing as
// a security hole nobody notices.
func (a AuthConfig) validate(isProduction bool) error {
	if err := a.JWT.validate(isProduction); err != nil {
		return err
	}
	return a.Password.validate(isProduction)
}

func (j JWTConfig) validate(isProduction bool) error {
	if j.Secret == "" {
		return fmt.Errorf(
			"config: auth.jwt.secret is required; generate one with: openssl rand -base64 48")
	}

	if len(j.Secret) < MinSecretLength {
		return fmt.Errorf(
			"config: auth.jwt.secret must be at least %d characters (got %d); "+
				"generate one with: openssl rand -base64 48",
			MinSecretLength, len(j.Secret))
	}

	if _, forbidden := forbiddenSecrets[strings.ToLower(strings.TrimSpace(j.Secret))]; forbidden {
		return fmt.Errorf("config: auth.jwt.secret is a well-known placeholder value")
	}

	if j.Issuer == "" {
		return fmt.Errorf("config: auth.jwt.issuer is required")
	}
	if j.Audience == "" {
		return fmt.Errorf("config: auth.jwt.audience is required")
	}

	if j.AccessTokenTTL <= 0 {
		return fmt.Errorf("config: auth.jwt.access_token_ttl must be positive")
	}
	if j.RefreshTokenTTL <= j.AccessTokenTTL {
		// A refresh token that expires no later than the access token it
		// refreshes is useless: the session would end at the first rotation.
		return fmt.Errorf(
			"config: auth.jwt.refresh_token_ttl (%s) must exceed access_token_ttl (%s)",
			j.RefreshTokenTTL, j.AccessTokenTTL)
	}

	if isProduction {
		// An access token cannot be revoked, so its TTL is the window a stolen
		// one stays usable. An hour is already generous for a warehouse app
		// where a refresh is cheap and transparent.
		if j.AccessTokenTTL > maxProductionAccessTTL {
			return fmt.Errorf(
				"config: auth.jwt.access_token_ttl (%s) exceeds the %s production maximum; "+
					"access tokens cannot be revoked, so a long TTL is an unbounded exposure window",
				j.AccessTokenTTL, maxProductionAccessTTL)
		}
	}

	if j.ClockSkew < 0 {
		return fmt.Errorf("config: auth.jwt.clock_skew must not be negative")
	}
	if j.MaxSessionsPerUser < 0 {
		return fmt.Errorf("config: auth.jwt.max_sessions_per_user must not be negative")
	}

	return nil
}

func (p PasswordConfig) validate(isProduction bool) error {
	if p.BcryptCost < bcrypt.MinCost || p.BcryptCost > bcrypt.MaxCost {
		return fmt.Errorf(
			"config: auth.password.bcrypt_cost must be between %d and %d (got %d)",
			bcrypt.MinCost, bcrypt.MaxCost, p.BcryptCost)
	}

	if isProduction && p.BcryptCost < minProductionBcryptCost {
		// Cost is exponential: each increment doubles the work. Below 12, a
		// leaked hash table is worth cracking on commodity GPUs.
		return fmt.Errorf(
			"config: auth.password.bcrypt_cost must be at least %d in production (got %d)",
			minProductionBcryptCost, p.BcryptCost)
	}

	return nil
}

const (
	maxProductionAccessTTL  = time.Hour
	minProductionBcryptCost = 12
)
