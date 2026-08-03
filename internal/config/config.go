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

// AuthConfig controls token, cookie, and rate-limit behavior.
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
}

// DatabaseConfig controls the local SQLite database.
type DatabaseConfig struct{ Path string }

// StorageConfig controls local object storage and upload validation.
type StorageConfig struct {
	Root         string
	MaxBytes     int64
	AllowedMIMEs []string
}

// Load reads configuration from environment variables and applies safe defaults.
func Load() (Config, error) {
	cfg := Config{
		Environment: getString("APP_ENV", "development"),
		HTTP: HTTPConfig{
			Address:           getString("HTTP_ADDRESS", ":8080"),
			ReadHeaderTimeout: getDuration("HTTP_READ_HEADER_TIMEOUT", 5*time.Second),
			ReadTimeout:       getDuration("HTTP_READ_TIMEOUT", 15*time.Second),
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
		},
		Database: DatabaseConfig{Path: getString("DATABASE_PATH", "data/fileservice.db")},
		Storage:  StorageConfig{Root: getString("STORAGE_ROOT", "data/objects"), MaxBytes: getInt64("UPLOAD_MAX_BYTES", 100<<20), AllowedMIMEs: []string{"image/jpeg", "image/png", "image/gif", "application/pdf", "text/plain", "application/zip"}},
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
