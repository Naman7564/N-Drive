package config

import (
	"os"
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	for _, key := range []string{"APP_ENV", "HTTP_ADDRESS", "HTTP_READ_HEADER_TIMEOUT", "HTTP_READ_TIMEOUT", "HTTP_WRITE_TIMEOUT", "HTTP_IDLE_TIMEOUT", "HTTP_SHUTDOWN_TIMEOUT", "HTTP_MAX_HEADER_BYTES", "N_DRIVE_USERNAME", "N_DRIVE_PASSWORD"} {
		t.Setenv(key, "")
		if err := os.Unsetenv(key); err != nil {
			t.Fatal(err)
		}
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.HTTP.Address != ":8080" || cfg.HTTP.MaxHeaderBytes != 1<<20 {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	if cfg.HTTP.ReadTimeout != 30*time.Minute {
		t.Fatalf("ReadTimeout = %s, want 30m", cfg.HTTP.ReadTimeout)
	}
	if cfg.Storage.MaxBytes != 5<<30 {
		t.Fatalf("MaxBytes = %d, want %d", cfg.Storage.MaxBytes, int64(5<<30))
	}
	if len(cfg.Storage.AllowedMIMEs) != 0 {
		t.Fatalf("AllowedMIMEs = %v, want empty for broad file support", cfg.Storage.AllowedMIMEs)
	}
	if cfg.Auth.Username != "Naman" || cfg.Auth.Password != "7564" {
		t.Fatalf("unexpected account defaults: username = %q, password = %q", cfg.Auth.Username, cfg.Auth.Password)
	}
}

func TestLoadReadsAccountFromEnvironment(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	t.Setenv("N_DRIVE_USERNAME", "alice")
	t.Setenv("N_DRIVE_PASSWORD", "hunter2-secret")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Auth.Username != "alice" || cfg.Auth.Password != "hunter2-secret" {
		t.Fatalf("account = %q / %q, want alice / hunter2-secret", cfg.Auth.Username, cfg.Auth.Password)
	}
}

func TestLoadRejectsDefaultPasswordInProduction(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	t.Setenv("SECURE_COOKIES", "true")
	t.Setenv("JWT_SECRET", "01234567890123456789012345678901")

	t.Setenv("N_DRIVE_PASSWORD", "7564")
	if _, err := Load(); err == nil {
		t.Fatal("Load() error = nil, want error when N_DRIVE_PASSWORD is the built-in default in production")
	}

	t.Setenv("N_DRIVE_PASSWORD", "short")
	if _, err := Load(); err == nil {
		t.Fatal("Load() error = nil, want error for a too-short password in production")
	}

	t.Setenv("N_DRIVE_PASSWORD", "correct-horse-battery")
	if _, err := Load(); err != nil {
		t.Fatalf("Load() with a real password error = %v", err)
	}
}

func TestLoadRejectsEmptyUsername(t *testing.T) {
	t.Setenv("N_DRIVE_USERNAME", "")
	if _, err := Load(); err == nil {
		t.Fatal("Load() error = nil, want error for empty N_DRIVE_USERNAME")
	}
}

func TestLoadRejectsInvalidDuration(t *testing.T) {
	t.Setenv("HTTP_READ_TIMEOUT", "not-a-duration")
	defer os.Unsetenv("HTTP_READ_TIMEOUT")

	if _, err := Load(); err == nil {
		t.Fatal("Load() error = nil, want invalid duration error")
	}
}
