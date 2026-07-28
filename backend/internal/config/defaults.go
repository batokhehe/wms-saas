package config

import (
	"errors"
	"io/fs"
	"os"
	"time"

	"github.com/go-viper/mapstructure/v2"
	"github.com/spf13/viper"
)

// setDefaults registers every configuration key with a sane development value.
//
// Registering defaults is not only about convenience: viper.AutomaticEnv only
// resolves keys it already knows about when Unmarshal walks the struct, so a
// key without a default would never be populated from the environment.
func setDefaults(v *viper.Viper) {
	// Application
	v.SetDefault("app.name", "wms-saas")
	v.SetDefault("app.env", "development")
	v.SetDefault("app.version", "0.1.0")
	v.SetDefault("app.timezone", "UTC")

	// HTTP server
	v.SetDefault("http.host", "0.0.0.0")
	v.SetDefault("http.port", 8080)
	v.SetDefault("http.read_timeout", 15*time.Second)
	v.SetDefault("http.write_timeout", 30*time.Second)
	v.SetDefault("http.idle_timeout", 60*time.Second)
	v.SetDefault("http.shutdown_timeout", 20*time.Second)
	v.SetDefault("http.allowed_origins", []string{"*"})

	// PostgreSQL
	v.SetDefault("database.host", "localhost")
	v.SetDefault("database.port", 5432)
	v.SetDefault("database.user", "wms")
	// No default password. A credential must never have a fallback value: a
	// production deploy that forgets to set DATABASE_PASSWORD has to fail
	// loudly rather than quietly authenticate with a well-known dev secret.
	v.SetDefault("database.password", "")
	v.SetDefault("database.name", "wms")
	v.SetDefault("database.ssl_mode", "disable")
	v.SetDefault("database.max_open_conns", 25)
	v.SetDefault("database.max_idle_conns", 10)
	v.SetDefault("database.conn_max_lifetime", time.Hour)
	v.SetDefault("database.conn_max_idle_time", 10*time.Minute)
	v.SetDefault("database.log_level", "warn")
	v.SetDefault("database.slow_threshold", 200*time.Millisecond)

	// Redis
	v.SetDefault("redis.host", "localhost")
	v.SetDefault("redis.port", 6379)
	v.SetDefault("redis.password", "")
	v.SetDefault("redis.db", 0)
	v.SetDefault("redis.pool_size", 20)
	v.SetDefault("redis.min_idle_conns", 5)
	v.SetDefault("redis.dial_timeout", 5*time.Second)
	v.SetDefault("redis.read_timeout", 3*time.Second)
	v.SetDefault("redis.write_timeout", 3*time.Second)

	// Logging
	v.SetDefault("log.level", "info")
	v.SetDefault("log.format", "console")

	// Authentication.
	//
	// No default is registered for auth.jwt.secret. Like database.password, a
	// credential must never have a fallback value: a deploy that forgets to set
	// it has to fail loudly rather than quietly sign tokens with a value that
	// is in the repository. The key is still registered below with an empty
	// string so AutomaticEnv can resolve it.
	v.SetDefault("auth.jwt.secret", "")
	v.SetDefault("auth.jwt.issuer", "wms-saas")
	v.SetDefault("auth.jwt.audience", "wms-saas-api")
	v.SetDefault("auth.jwt.access_token_ttl", 15*time.Minute)
	v.SetDefault("auth.jwt.refresh_token_ttl", 7*24*time.Hour)
	v.SetDefault("auth.jwt.clock_skew", 30*time.Second)
	v.SetDefault("auth.jwt.max_sessions_per_user", 10)

	// Cost 12 is roughly 250ms on current server hardware: slow enough to make
	// offline cracking expensive, fast enough that login latency stays
	// acceptable. It is also the production floor enforced by validation.
	v.SetDefault("auth.password.bcrypt_cost", 12)
}

// decodeHook converts the flat string values that come out of .env files and
// environment variables into the richer types used by Config.
func decodeHook() mapstructure.DecodeHookFunc {
	return mapstructure.ComposeDecodeHookFunc(
		// "30s", "1h" -> time.Duration
		mapstructure.StringToTimeDurationHookFunc(),
		// "a,b,c" -> []string
		mapstructure.StringToSliceHookFunc(","),
	)
}

// isNotExist reports whether err is a "file does not exist" error, which is the
// expected outcome when no .env file is shipped with the container.
func isNotExist(err error) bool {
	return errors.Is(err, os.ErrNotExist) || errors.Is(err, fs.ErrNotExist)
}

// asConfigFileNotFound reports whether err is viper's own not-found error.
func asConfigFileNotFound(err error, target *viper.ConfigFileNotFoundError) bool {
	return errors.As(err, target)
}
