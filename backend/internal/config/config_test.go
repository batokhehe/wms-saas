package config

import (
	"testing"
	"time"
)

// testSecret satisfies the auth.jwt.secret requirement. Like database.password
// it has no default, so every Load() in these tests must supply one.
const testSecret = "test-secret-that-is-at-least-32-characters-long"

// withSecret sets the mandatory signing secret for a test.
func withSecret(t *testing.T) {
	t.Helper()
	t.Setenv("AUTH_JWT_SECRET", testSecret)
}

// TestLoadDefaults verifies the service is bootable with no configuration at
// all, which is what makes a fresh clone run without setup.
func TestLoadDefaults(t *testing.T) {
	withSecret(t)

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}

	if got, want := cfg.HTTP.Port, 8080; got != want {
		t.Errorf("HTTP.Port = %d, want %d", got, want)
	}
	if got, want := cfg.HTTP.ShutdownTimeout, 20*time.Second; got != want {
		t.Errorf("HTTP.ShutdownTimeout = %v, want %v", got, want)
	}
	if got, want := cfg.App.Env, "development"; got != want {
		t.Errorf("App.Env = %q, want %q", got, want)
	}
}

// TestEnvOverridesDefault is the regression guard for the AutomaticEnv/Unmarshal
// interaction: without a registered default, an env-only key silently stays at
// its zero value.
func TestEnvOverridesDefault(t *testing.T) {
	withSecret(t)

	t.Setenv("HTTP_PORT", "9999")
	t.Setenv("DATABASE_HOST", "db.internal")
	t.Setenv("HTTP_READ_TIMEOUT", "45s")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}

	if got, want := cfg.HTTP.Port, 9999; got != want {
		t.Errorf("HTTP.Port = %d, want %d", got, want)
	}
	if got, want := cfg.Database.Host, "db.internal"; got != want {
		t.Errorf("Database.Host = %q, want %q", got, want)
	}
	// Proves the string -> time.Duration decode hook is wired.
	if got, want := cfg.HTTP.ReadTimeout, 45*time.Second; got != want {
		t.Errorf("HTTP.ReadTimeout = %v, want %v", got, want)
	}
}

// TestAllowedOriginsSlice covers the comma-separated string -> []string hook.
func TestAllowedOriginsSlice(t *testing.T) {
	withSecret(t)

	t.Setenv("HTTP_ALLOWED_ORIGINS", "https://a.example,https://b.example")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}

	if got, want := len(cfg.HTTP.AllowedOrigins), 2; got != want {
		t.Fatalf("len(AllowedOrigins) = %d, want %d (%v)", got, want, cfg.HTTP.AllowedOrigins)
	}
	if got, want := cfg.HTTP.AllowedOrigins[1], "https://b.example"; got != want {
		t.Errorf("AllowedOrigins[1] = %q, want %q", got, want)
	}
}

// TestValidationRejectsBadConfig checks that misconfiguration fails at boot
// rather than surfacing as a confusing error on the first request.
func TestValidationRejectsBadConfig(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
	}{
		{"unknown environment", map[string]string{"APP_ENV": "banana"}},
		{"port out of range", map[string]string{"HTTP_PORT": "70000"}},
		{"idle exceeds open conns", map[string]string{
			"DATABASE_MAX_OPEN_CONNS": "5",
			"DATABASE_MAX_IDLE_CONNS": "50",
		}},
		{"production forbids blank db password", map[string]string{
			"APP_ENV":              "production",
			"DATABASE_SSL_MODE":    "require",
			"DATABASE_PASSWORD":    "",
			"HTTP_ALLOWED_ORIGINS": "https://app.example",
		}},
		{"production forbids sslmode disable", map[string]string{
			"APP_ENV":              "production",
			"DATABASE_PASSWORD":    "secret",
			"DATABASE_SSL_MODE":    "disable",
			"HTTP_ALLOWED_ORIGINS": "https://app.example",
		}},
		{"production forbids wildcard cors", map[string]string{
			"APP_ENV":              "production",
			"DATABASE_PASSWORD":    "secret",
			"DATABASE_SSL_MODE":    "require",
			"HTTP_ALLOWED_ORIGINS": "*",
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withSecret(t)
			for k, v := range tt.env {
				t.Setenv(k, v)
			}

			if _, err := Load(""); err == nil {
				t.Error("Load() error = nil, want a validation error")
			}
		})
	}
}

// TestMissingEnvFileIsNotAnError covers the container case, where configuration
// arrives as plain environment variables and no .env is shipped.
func TestMissingEnvFileIsNotAnError(t *testing.T) {
	withSecret(t)

	if _, err := Load("does-not-exist.env"); err != nil {
		t.Errorf("Load() error = %v, want nil for a missing .env", err)
	}
}
