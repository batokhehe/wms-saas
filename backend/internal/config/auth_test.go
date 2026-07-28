package config

import (
	"strings"
	"testing"
)

// TestAuthValidationRejectsWeakSecrets covers the boot-time guards that stop a
// deployment signing tokens with something an attacker can guess or brute-force.
func TestAuthValidationRejectsWeakSecrets(t *testing.T) {
	tests := map[string]string{
		"empty":              "",
		"too short":          "short-secret",
		"exactly one under":  strings.Repeat("a", MinSecretLength-1),
		"known placeholder":  "secret",
		"placeholder padded": "changeme",
		"placeholder cased":  "ChangeMe",
	}

	for name, secret := range tests {
		t.Run(name, func(t *testing.T) {
			withSecret(t)
			t.Setenv("AUTH_JWT_SECRET", secret)

			if _, err := Load(""); err == nil {
				t.Errorf("Load() = nil for secret %q, want a validation error", secret)
			}
		})
	}
}

func TestAuthValidationAcceptsStrongSecret(t *testing.T) {
	withSecret(t)
	t.Setenv("AUTH_JWT_SECRET", strings.Repeat("x", MinSecretLength))

	if _, err := Load(""); err != nil {
		t.Errorf("Load() = %v, want nil for a secret at the minimum length", err)
	}
}

// TestRefreshTTLMustExceedAccessTTL: a refresh token expiring no later than the
// access token it refreshes is useless — the session would end at the first
// rotation.
func TestRefreshTTLMustExceedAccessTTL(t *testing.T) {
	withSecret(t)
	t.Setenv("AUTH_JWT_ACCESS_TOKEN_TTL", "1h")
	t.Setenv("AUTH_JWT_REFRESH_TOKEN_TTL", "30m")

	if _, err := Load(""); err == nil {
		t.Error("Load() = nil for refresh_ttl <= access_ttl, want an error")
	}
}

func TestBcryptCostBounds(t *testing.T) {
	for _, cost := range []string{"1", "3", "32"} {
		t.Run(cost, func(t *testing.T) {
			withSecret(t)
			t.Setenv("AUTH_PASSWORD_BCRYPT_COST", cost)

			if _, err := Load(""); err == nil {
				t.Errorf("Load() = nil for bcrypt cost %s, want an error", cost)
			}
		})
	}
}

// TestProductionHardening covers the rules that only apply under
// APP_ENV=production, where a permissive development value becomes a real risk.
func TestProductionHardening(t *testing.T) {
	base := map[string]string{
		"APP_ENV":              "production",
		"DATABASE_PASSWORD":    "a-real-password",
		"DATABASE_SSL_MODE":    "require",
		"HTTP_ALLOWED_ORIGINS": "https://app.example",
	}

	tests := map[string]map[string]string{
		// An access token cannot be revoked, so its TTL is the window a stolen
		// one stays usable.
		"access ttl too long": {"AUTH_JWT_ACCESS_TOKEN_TTL": "24h", "AUTH_JWT_REFRESH_TOKEN_TTL": "168h"},
		// Below cost 12 a leaked hash table is worth cracking on commodity GPUs.
		"bcrypt cost too low": {"AUTH_PASSWORD_BCRYPT_COST": "10"},
	}

	for name, overrides := range tests {
		t.Run(name, func(t *testing.T) {
			withSecret(t)
			for k, v := range base {
				t.Setenv(k, v)
			}
			for k, v := range overrides {
				t.Setenv(k, v)
			}

			if _, err := Load(""); err == nil {
				t.Error("Load() = nil, want a production hardening error")
			}
		})
	}
}

// TestProductionAcceptsSaneAuthConfig is the counterpart: the documented
// production values must actually pass.
func TestProductionAcceptsSaneAuthConfig(t *testing.T) {
	withSecret(t)
	for k, v := range map[string]string{
		"APP_ENV":                    "production",
		"DATABASE_PASSWORD":          "a-real-password",
		"DATABASE_SSL_MODE":          "require",
		"HTTP_ALLOWED_ORIGINS":       "https://app.example",
		"AUTH_JWT_ACCESS_TOKEN_TTL":  "15m",
		"AUTH_JWT_REFRESH_TOKEN_TTL": "168h",
		"AUTH_PASSWORD_BCRYPT_COST":  "12",
	} {
		t.Setenv(k, v)
	}

	if _, err := Load(""); err != nil {
		t.Errorf("Load() = %v, want nil for a valid production config", err)
	}
}

func TestAuthDefaults(t *testing.T) {
	withSecret(t)

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() = %v", err)
	}

	if cfg.Auth.Password.BcryptCost != 12 {
		t.Errorf("BcryptCost = %d, want 12", cfg.Auth.Password.BcryptCost)
	}
	if cfg.Auth.JWT.AccessTokenTTL.Minutes() != 15 {
		t.Errorf("AccessTokenTTL = %v, want 15m", cfg.Auth.JWT.AccessTokenTTL)
	}
	if cfg.Auth.JWT.Issuer == "" || cfg.Auth.JWT.Audience == "" {
		t.Error("issuer and audience must have defaults")
	}
}
