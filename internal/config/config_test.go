package config

import (
	"os"
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	for _, key := range []string{"APP_ENV", "HTTP_ADDRESS", "HTTP_READ_HEADER_TIMEOUT", "HTTP_READ_TIMEOUT", "HTTP_WRITE_TIMEOUT", "HTTP_IDLE_TIMEOUT", "HTTP_SHUTDOWN_TIMEOUT", "HTTP_MAX_HEADER_BYTES"} {
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
}

func TestLoadRejectsInvalidDuration(t *testing.T) {
	t.Setenv("HTTP_READ_TIMEOUT", "not-a-duration")
	defer os.Unsetenv("HTTP_READ_TIMEOUT")

	if _, err := Load(); err == nil {
		t.Fatal("Load() error = nil, want invalid duration error")
	}
}
