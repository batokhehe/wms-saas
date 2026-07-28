// Package config loads and validates all runtime configuration for the service.
//
// Precedence (highest wins): real environment variables > .env file > built-in
// defaults. Every key is registered as a default so that viper.Unmarshal can
// still see values that only exist in the environment.
package config

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// Config is the root configuration object. It is loaded once at startup and
// then passed by value into the components that need it.
type Config struct {
	App      AppConfig      `mapstructure:"app"`
	HTTP     HTTPConfig     `mapstructure:"http"`
	Database DatabaseConfig `mapstructure:"database"`
	Redis    RedisConfig    `mapstructure:"redis"`
	Log      LogConfig      `mapstructure:"log"`
	Auth     AuthConfig     `mapstructure:"auth"`
}

// AuthConfig configures identity: token signing and password hashing.
//
// Nothing here has a usable production default. Both the signing secret and the
// hashing cost are deployment decisions, and shipping a fallback for either
// would mean a misconfigured deploy silently runs with a known secret rather
// than failing.
type AuthConfig struct {
	JWT      JWTConfig      `mapstructure:"jwt"`
	Password PasswordConfig `mapstructure:"password"`
}

// JWTConfig configures access and refresh token issuance.
type JWTConfig struct {
	// Secret signs and verifies access tokens. There is no default: see
	// validate() for the rules enforced on it.
	Secret string `mapstructure:"secret"`

	// Issuer and Audience are stamped into every token and checked on every
	// verification. They stop a token minted by a sibling service — sharing the
	// same secret through a copy-paste of the deployment config — from being
	// accepted here.
	Issuer   string `mapstructure:"issuer"`
	Audience string `mapstructure:"audience"`

	// AccessTokenTTL is deliberately short. An access token cannot be revoked
	// (that is the point of a stateless token), so its lifetime is the window
	// during which a stolen one remains useful.
	AccessTokenTTL time.Duration `mapstructure:"access_token_ttl"`

	// RefreshTokenTTL bounds a session. Refresh tokens are stored server-side
	// and can be revoked, so this can be long.
	RefreshTokenTTL time.Duration `mapstructure:"refresh_token_ttl"`

	// ClockSkew tolerates small clock differences between the issuing and
	// verifying process when checking exp and nbf.
	ClockSkew time.Duration `mapstructure:"clock_skew"`

	// MaxSessionsPerUser caps concurrent live refresh tokens per user. Zero
	// means unlimited. It bounds the damage of a credential-stuffing run that
	// succeeds: an attacker cannot accumulate unlimited long-lived sessions.
	MaxSessionsPerUser int `mapstructure:"max_sessions_per_user"`
}

// PasswordConfig configures bcrypt.
type PasswordConfig struct {
	// BcryptCost is the work factor. Higher is slower to crack and slower to
	// verify; the right value is the highest one that keeps login latency
	// acceptable on production hardware.
	BcryptCost int `mapstructure:"bcrypt_cost"`
}

// AppConfig holds process-level identity and behaviour flags.
type AppConfig struct {
	Name string `mapstructure:"name"`
	// Env is one of: development, staging, production.
	Env      string `mapstructure:"env"`
	Version  string `mapstructure:"version"`
	Timezone string `mapstructure:"timezone"`
}

// IsProduction reports whether the process runs with production semantics
// (JSON logs, Gin release mode, no stack traces in responses).
func (a AppConfig) IsProduction() bool {
	return strings.EqualFold(a.Env, "production")
}

// HTTPConfig configures the Gin server and its shutdown behaviour.
type HTTPConfig struct {
	Host string `mapstructure:"host"`
	Port int    `mapstructure:"port"`

	ReadTimeout  time.Duration `mapstructure:"read_timeout"`
	WriteTimeout time.Duration `mapstructure:"write_timeout"`
	IdleTimeout  time.Duration `mapstructure:"idle_timeout"`

	// ShutdownTimeout bounds how long in-flight requests may finish after a
	// termination signal before the server is forced closed.
	ShutdownTimeout time.Duration `mapstructure:"shutdown_timeout"`

	// AllowedOrigins is the CORS allow-list. A single "*" allows everything,
	// which is rejected in production.
	AllowedOrigins []string `mapstructure:"allowed_origins"`
}

// Address returns the host:port the server binds to.
func (h HTTPConfig) Address() string {
	return fmt.Sprintf("%s:%d", h.Host, h.Port)
}

// DatabaseConfig configures the PostgreSQL connection and its pool.
type DatabaseConfig struct {
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	User     string `mapstructure:"user"`
	Password string `mapstructure:"password"`
	Name     string `mapstructure:"name"`
	SSLMode  string `mapstructure:"ssl_mode"`

	MaxOpenConns    int           `mapstructure:"max_open_conns"`
	MaxIdleConns    int           `mapstructure:"max_idle_conns"`
	ConnMaxLifetime time.Duration `mapstructure:"conn_max_lifetime"`
	ConnMaxIdleTime time.Duration `mapstructure:"conn_max_idle_time"`

	// LogLevel maps to GORM's logger: silent, error, warn, info.
	LogLevel string `mapstructure:"log_level"`
	// SlowThreshold marks queries slower than this as slow in the GORM log.
	SlowThreshold time.Duration `mapstructure:"slow_threshold"`
}

// DSN builds the libpq key/value connection string used by GORM's pgx driver.
func (d DatabaseConfig) DSN() string {
	return fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s TimeZone=UTC",
		d.Host, d.Port, d.User, d.Password, d.Name, d.SSLMode,
	)
}

// URL builds the same connection as a postgres:// URL.
//
// golang-migrate parses a URL and rejects the key/value form outright, so both
// renderings are needed. Credentials go through url.UserPassword rather than
// string concatenation: a password containing "@", "/" or ":" would otherwise
// silently produce a URL pointing at the wrong host.
func (d DatabaseConfig) URL() string {
	query := url.Values{}
	query.Set("sslmode", d.SSLMode)
	query.Set("TimeZone", "UTC")

	u := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(d.User, d.Password),
		Host:     fmt.Sprintf("%s:%d", d.Host, d.Port),
		Path:     "/" + d.Name,
		RawQuery: query.Encode(),
	}

	return u.String()
}

// RedisConfig configures the shared Redis client. The same instance backs both
// the cache and the Asynq queue, separated by logical DB index.
type RedisConfig struct {
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	Password string `mapstructure:"password"`
	DB       int    `mapstructure:"db"`

	PoolSize     int           `mapstructure:"pool_size"`
	MinIdleConns int           `mapstructure:"min_idle_conns"`
	DialTimeout  time.Duration `mapstructure:"dial_timeout"`
	ReadTimeout  time.Duration `mapstructure:"read_timeout"`
	WriteTimeout time.Duration `mapstructure:"write_timeout"`
}

// Address returns the host:port of the Redis server.
func (r RedisConfig) Address() string {
	return fmt.Sprintf("%s:%d", r.Host, r.Port)
}

// LogConfig configures the Zap logger.
type LogConfig struct {
	// Level is one of: debug, info, warn, error.
	Level string `mapstructure:"level"`
	// Format is either "json" or "console".
	Format string `mapstructure:"format"`
}

// LoadOption adjusts which configuration sections a binary requires.
type LoadOption func(*loadOptions)

type loadOptions struct {
	skipAuth bool
}

// WithoutAuth skips validation of the auth section.
//
// Not every binary is an identity provider. The migration runner touches only
// the database, and requiring it to carry a JWT signing secret would mean
// either shipping a placeholder credential in the deployment manifest or
// granting a schema-migration job access to the real one. Both are worse than
// letting each binary declare what it actually needs.
func WithoutAuth() LoadOption {
	return func(o *loadOptions) { o.skipAuth = true }
}

// Load reads configuration from the given .env file (if present), overlays real
// environment variables, and returns a validated Config.
//
// A missing .env file is not an error: containerised deployments inject plain
// environment variables instead.
func Load(envFile string, opts ...LoadOption) (*Config, error) {
	var options loadOptions
	for _, opt := range opts {
		opt(&options)
	}

	v := viper.New()

	setDefaults(v)

	// Map nested keys such as "database.host" onto DATABASE_HOST.
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	// Honour explicitly-empty environment variables. Without this, viper treats
	// FOO="" as unset and silently falls back to the default, which would let a
	// deliberately blanked credential resolve to a built-in value.
	v.AllowEmptyEnv(true)
	v.AutomaticEnv()

	if envFile != "" {
		v.SetConfigFile(envFile)
		v.SetConfigType("env")

		if err := v.ReadInConfig(); err != nil {
			var notFound viper.ConfigFileNotFoundError
			// A file that was named explicitly but does not exist surfaces as a
			// *fs.PathError rather than ConfigFileNotFoundError, so both cases
			// are tolerated here and only real parse errors are returned.
			if !isNotExist(err) && !asConfigFileNotFound(err, &notFound) {
				return nil, fmt.Errorf("config: reading %s: %w", envFile, err)
			}
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg, viper.DecodeHook(decodeHook())); err != nil {
		return nil, fmt.Errorf("config: decoding: %w", err)
	}

	if err := cfg.validate(options); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// validate rejects configurations that would fail later in a confusing way, so
// that misconfiguration is caught at boot rather than on the first request.
func (c *Config) validate(options loadOptions) error {
	if c.App.Name == "" {
		return fmt.Errorf("config: app.name must not be empty")
	}

	switch strings.ToLower(c.App.Env) {
	case "development", "staging", "production":
	default:
		return fmt.Errorf("config: app.env %q must be development, staging or production", c.App.Env)
	}

	if c.HTTP.Port < 1 || c.HTTP.Port > 65535 {
		return fmt.Errorf("config: http.port %d is out of range", c.HTTP.Port)
	}

	if c.Database.Host == "" || c.Database.Name == "" || c.Database.User == "" {
		return fmt.Errorf("config: database host, name and user are required")
	}

	if c.Redis.Host == "" {
		return fmt.Errorf("config: redis.host is required")
	}

	if c.Database.MaxIdleConns > c.Database.MaxOpenConns {
		return fmt.Errorf(
			"config: database.max_idle_conns (%d) must not exceed max_open_conns (%d)",
			c.Database.MaxIdleConns, c.Database.MaxOpenConns,
		)
	}

	if !options.skipAuth {
		if err := c.Auth.validate(c.App.IsProduction()); err != nil {
			return err
		}
	}

	if c.App.IsProduction() {
		if c.Database.Password == "" {
			return fmt.Errorf("config: database.password is required in production")
		}
		if c.Database.SSLMode == "disable" {
			return fmt.Errorf("config: database.ssl_mode must not be disable in production")
		}
		for _, origin := range c.HTTP.AllowedOrigins {
			if origin == "*" {
				return fmt.Errorf("config: http.allowed_origins must not be * in production")
			}
		}
	}

	return nil
}
