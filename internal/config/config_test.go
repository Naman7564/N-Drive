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

func TestLoadParsesCORSOrigins(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://files.example.com, https://api2.example.com/")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.CORS.AllowedOrigins) != 2 {
		t.Fatalf("origins = %v, want 2 entries", cfg.CORS.AllowedOrigins)
	}
	if cfg.CORS.AllowedOrigins[0] != "https://files.example.com" || cfg.CORS.AllowedOrigins[1] != "https://api2.example.com" {
		t.Fatalf("origins = %v", cfg.CORS.AllowedOrigins)
	}
}

func TestLoadRejectsInvalidCORSOrigin(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	for _, value := range []string{"not-a-url", "https://files.example.com/path"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("CORS_ALLOWED_ORIGINS", value)
			if _, err := Load(); err == nil {
				t.Fatalf("Load() with CORS_ALLOWED_ORIGINS=%q error = nil, want error", value)
			}
		})
	}
}

func TestLoadReadsUIAPIBasе(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	t.Setenv("UI_API_BASE", "https://api.example.com/")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.UI.APIBase != "https://api.example.com" {
		t.Fatalf("UI.APIBase = %q, want trailing slash trimmed", cfg.UI.APIBase)
	}

	t.Setenv("UI_API_BASE", "relative/path")
	if _, err := Load(); err == nil {
		t.Fatal("Load() error = nil, want error for a relative UI_API_BASE")
	}
}

func TestLoadParsesRemoteServers(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	t.Setenv("UI_REMOTE_SERVERS", "Media=http://130.210.21.47:8080;Backup=https://backup.example.com/")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.UI.RemoteServers) != 2 {
		t.Fatalf("remote servers = %v, want 2 entries", cfg.UI.RemoteServers)
	}
	if cfg.UI.RemoteServers[0].Name != "Media" || cfg.UI.RemoteServers[0].Base != "http://130.210.21.47:8080" {
		t.Fatalf("first remote server = %+v", cfg.UI.RemoteServers[0])
	}
	if cfg.UI.RemoteServers[1].Name != "Backup" || cfg.UI.RemoteServers[1].Base != "https://backup.example.com" {
		t.Fatalf("second remote server = %+v, want trailing slash trimmed", cfg.UI.RemoteServers[1])
	}
}

func TestLoadRejectsInvalidRemoteServers(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	cases := []string{
		"Bad Name=http://host", // spaces in the name
		"Name=not-a-url",       // invalid base
		"=http://host",         // missing name
		"Name=",                // missing base
		"Name=/relative",       // relative base
		"A=http://a.example.com;A=http://b.example.com",           // duplicate name
		"A=http://130.210.21.47:8080;B=http://130.210.21.47:8080", // duplicate base URL
	}
	for _, value := range cases {
		t.Run(value, func(t *testing.T) {
			t.Setenv("UI_REMOTE_SERVERS", value)
			if _, err := Load(); err == nil {
				t.Fatalf("Load() with UI_REMOTE_SERVERS=%q error = nil, want error", value)
			}
		})
	}
}

func TestLoadDefaultsToSingleMountFromStorageRoot(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	t.Setenv("STORAGE_MOUNTS", "")
	t.Setenv("STORAGE_ROOT", "/mnt/ndrive")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.Storage.Mounts) != 1 {
		t.Fatalf("mounts = %v, want a single default mount", cfg.Storage.Mounts)
	}
	mount := cfg.Storage.Mounts[0]
	if mount.ID != "default" || mount.Name != "Main" || mount.Root != "/mnt/ndrive" {
		t.Fatalf("default mount = %+v", mount)
	}
}

func TestLoadParsesStorageMounts(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	t.Setenv("STORAGE_MOUNTS", "Main=/mnt/main;Media=/mnt/media")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.Storage.Mounts) != 2 {
		t.Fatalf("mounts = %v, want 2 mounts", cfg.Storage.Mounts)
	}
	// The first listed disk is the default disk and always keeps the id
	// "default", so pre-multi-disk files remain visible on it.
	if cfg.Storage.Mounts[0].ID != "default" || cfg.Storage.Mounts[0].Name != "Main" || cfg.Storage.Mounts[0].Root != "/mnt/main" {
		t.Fatalf("first mount = %+v", cfg.Storage.Mounts[0])
	}
	if cfg.Storage.Mounts[1].ID != "Media" || cfg.Storage.Mounts[1].Root != "/mnt/media" {
		t.Fatalf("second mount = %+v", cfg.Storage.Mounts[1])
	}
}

func TestLoadRejectsInvalidStorageMounts(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	cases := []string{
		"Not Allowed=/x", // spaces in the name
		"=/x",            // missing name
		"Name=",          // missing path
		"Name=/x;= /y",   // one bad entry
		"Na@me=/x",       // name with forbidden characters
	}
	for _, value := range cases {
		t.Run(value, func(t *testing.T) {
			t.Setenv("STORAGE_MOUNTS", value)
			if _, err := Load(); err == nil {
				t.Fatalf("Load() with STORAGE_MOUNTS=%q error = nil, want error", value)
			}
		})
	}
}

func TestLoadRespectsExplicitDefaultMountName(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	// When the user names a disk "default" themselves, the first disk keeps
	// its own id instead of being remapped onto a duplicate "default".
	t.Setenv("STORAGE_MOUNTS", "Main=/mnt/main;default=/mnt/default")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.Storage.Mounts) != 2 {
		t.Fatalf("mounts = %v, want 2 mounts", cfg.Storage.Mounts)
	}
	if cfg.Storage.Mounts[0].ID != "Main" || cfg.Storage.Mounts[1].ID != "default" {
		t.Fatalf("mounts = %+v, want Main then default", cfg.Storage.Mounts)
	}
}

func TestLoadAllowsTrailingMountSeparator(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	t.Setenv("STORAGE_MOUNTS", "Main=/mnt/main;")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.Storage.Mounts) != 1 || cfg.Storage.Mounts[0].ID != "default" || cfg.Storage.Mounts[0].Name != "Main" {
		t.Fatalf("mounts = %v, want one default Main mount", cfg.Storage.Mounts)
	}
}
