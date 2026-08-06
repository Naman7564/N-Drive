package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config contains runtime configuration for the API.
type Config struct {
	Environment string
	HTTP        HTTPConfig
	Auth        AuthConfig
	Database    DatabaseConfig
	Storage     StorageConfig
}

// HTTPConfig controls server networking and timeout behavior.
type HTTPConfig struct {
	Address           string
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	ShutdownTimeout   time.Duration
	MaxHeaderBytes    int
}

// AuthConfig controls token, cookie, rate-limit, and account behavior.
type AuthConfig struct {
	JWTSecret         string
	JWTIssuer         string
	AccessTokenTTL    time.Duration
	RefreshTokenTTL   time.Duration
	SecureCookies     bool
	CookieDomain      string
	LoginRateLimit    int
	LoginRateWindow   time.Duration
	RefreshRateLimit  int
	RefreshRateWindow time.Duration
	// Username and Password seed the single user account when the database
	// has no users yet. They are read from N_DRIVE_USERNAME / N_DRIVE_PASSWORD;
	// the built-in values exist only for local development and are rejected
	// in production.
	Username string
	Password string
}

// Default account credentials, used only when the environment variables are
// unset. The password default is intentionally weak: it must never be used
// outside local development.
const (
	defaultUsername = "Naman"
	defaultPassword = "7564"
)

// DatabaseConfig controls the local SQLite database.
type DatabaseConfig struct{ Path string }

// StorageConfig controls local object storage and upload validation.
type StorageConfig struct {
	Root     string
	MaxBytes int64
	// AllowedMIMEs is an optional allowlist. An empty list accepts all file types.
	AllowedMIMEs []string
}

// Load reads configuration from environment variables and applies safe defaults.
func Load() (Config, error) {
	cfg := Config{
		Environment: getString("APP_ENV", "development"),
		HTTP: HTTPConfig{
			Address:           getString("HTTP_ADDRESS", ":8080"),
			ReadHeaderTimeout: getDuration("HTTP_READ_HEADER_TIMEOUT", 5*time.Second),
			ReadTimeout:       getDuration("HTTP_READ_TIMEOUT", 30*time.Minute),
			WriteTimeout:      getDuration("HTTP_WRITE_TIMEOUT", 30*time.Second),
			IdleTimeout:       getDuration("HTTP_IDLE_TIMEOUT", 60*time.Second),
			ShutdownTimeout:   getDuration("HTTP_SHUTDOWN_TIMEOUT", 10*time.Second),
			MaxHeaderBytes:    getInt("HTTP_MAX_HEADER_BYTES", 1<<20),
		},
		Auth: AuthConfig{
			JWTSecret:         getString("JWT_SECRET", "development-only-change-me-please-32-bytes!"),
			JWTIssuer:         getString("JWT_ISSUER", "fileservice"),
			AccessTokenTTL:    getDuration("JWT_ACCESS_TTL", 15*time.Minute),
			RefreshTokenTTL:   getDuration("JWT_REFRESH_TTL", 7*24*time.Hour),
			SecureCookies:     getBool("SECURE_COOKIES", false),
			CookieDomain:      getString("COOKIE_DOMAIN", ""),
			LoginRateLimit:    getInt("LOGIN_RATE_LIMIT", 5),
			LoginRateWindow:   getDuration("LOGIN_RATE_WINDOW", time.Minute),
			RefreshRateLimit:  getInt("REFRESH_RATE_LIMIT", 10),
			RefreshRateWindow: getDuration("REFRESH_RATE_WINDOW", time.Minute),
			Username:          getString("N_DRIVE_USERNAME", defaultUsername),
			Password:          getString("N_DRIVE_PASSWORD", defaultPassword),
		},
		Database: DatabaseConfig{Path: getString("DATABASE_PATH", "data/fileservice.db")},
		Storage:  StorageConfig{Root: getString("STORAGE_ROOT", "data/objects"), MaxBytes: getInt64("UPLOAD_MAX_BYTES", 5<<30), AllowedMIMEs: nil},
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Validate rejects unsafe or unusable runtime settings.
func (c Config) Validate() error {
	if strings.TrimSpace(c.Environment) == "" {
		return fmt.Errorf("APP_ENV must not be empty")
	}
	if strings.TrimSpace(c.HTTP.Address) == "" {
		return fmt.Errorf("HTTP_ADDRESS must not be empty")
	}
	if c.HTTP.ReadHeaderTimeout <= 0 || c.HTTP.ReadTimeout <= 0 || c.HTTP.WriteTimeout <= 0 || c.HTTP.IdleTimeout <= 0 || c.HTTP.ShutdownTimeout <= 0 {
		return fmt.Errorf("HTTP timeouts must be positive")
	}
	if c.HTTP.MaxHeaderBytes < 1024 {
		return fmt.Errorf("HTTP_MAX_HEADER_BYTES must be at least 1024")
	}
	if len(c.Auth.JWTSecret) < 32 {
		return fmt.Errorf("JWT_SECRET must be at least 32 bytes")
	}
	if strings.EqualFold(c.Environment, "production") && c.Auth.JWTSecret == "development-only-change-me-please-32-bytes!" {
		return fmt.Errorf("JWT_SECRET must be changed in production")
	}
	if strings.TrimSpace(c.Auth.Username) == "" {
		return fmt.Errorf("N_DRIVE_USERNAME must not be empty")
	}
	if c.Auth.Password == "" {
		return fmt.Errorf("N_DRIVE_PASSWORD must not be empty")
	}
	if strings.EqualFold(c.Environment, "production") {
		if c.Auth.Password == defaultPassword {
			return fmt.Errorf("N_DRIVE_PASSWORD must be changed in production")
		}
		if len(c.Auth.Password) < 8 {
			return fmt.Errorf("N_DRIVE_PASSWORD must be at least 8 characters in production")
		}
	}
	if strings.EqualFold(c.Environment, "production") && !c.Auth.SecureCookies {
		return fmt.Errorf("SECURE_COOKIES must be enabled in production")
	}
	if strings.TrimSpace(c.Auth.JWTIssuer) == "" {
		return fmt.Errorf("JWT_ISSUER must not be empty")
	}
	if c.Auth.AccessTokenTTL <= 0 || c.Auth.RefreshTokenTTL <= c.Auth.AccessTokenTTL {
		return fmt.Errorf("JWT token lifetimes are invalid")
	}
	if c.Auth.LoginRateLimit <= 0 || c.Auth.RefreshRateLimit <= 0 || c.Auth.LoginRateWindow <= 0 || c.Auth.RefreshRateWindow <= 0 {
		return fmt.Errorf("auth rate limits are invalid")
	}
	if strings.TrimSpace(c.Database.Path) == "" {
		return fmt.Errorf("DATABASE_PATH must not be empty")
	}
	if strings.TrimSpace(c.Storage.Root) == "" || c.Storage.MaxBytes <= 0 {
		return fmt.Errorf("storage configuration is invalid")
	}
	return nil
}

func getString(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func getDuration(key string, fallback time.Duration) time.Duration {
	value, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(value) == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0
	}
	return parsed
}

func getInt(key string, fallback int) int {
	value, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(value) == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0
	}
	return parsed
}

func getInt64(key string, fallback int64) int64 {
	value, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(value) == "" {
		return fallback
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0
	}
	return parsed
}

func getBool(key string, fallback bool) bool {
	value, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(value) == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false
	}
	return parsed
}
