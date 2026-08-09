package config

import (
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"fileservice/internal/storage"
)

// Config contains runtime configuration for the API.
type Config struct {
	Environment string
	HTTP        HTTPConfig
	Auth        AuthConfig
	Database    DatabaseConfig
	Storage     StorageConfig
	CORS        CORSConfig
	UI          UIConfig
}

// CORSConfig controls cross-origin API access for a separately hosted UI.
// AllowedOrigins lists the exact origins (scheme, host, optional port) that
// may call the API from the browser.
type CORSConfig struct {
	AllowedOrigins []string
}

// RemoteServer pairs a display name with the base URL of an additional
// backend. When UI_REMOTE_SERVERS is set, the built-in UI keeps talking to
// its own API and additionally lists each remote server's disks in the
// sidebar, so several servers appear in one workspace.
type RemoteServer struct {
	Name string
	Base string
}

// UIConfig controls how the built-in web UI targets its API. When APIBase is
// set, the embedded page points all its requests at that URL instead of
// itself, which pairs with CORSConfig to let the UI live on a different
// origin. RemoteServers is the multi-server variant: the UI stays pointed at
// its own API and lists each remote server's disks alongside the local ones.
// When RemoteServers is non-empty it takes precedence over APIBase.
type UIConfig struct {
	APIBase       string
	RemoteServers []RemoteServer
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
	// Mounts lists the named storage roots (disks) served by the app. When
	// empty, a single default mount is derived from Root.
	Mounts []storage.MountSpec
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
		CORS:     CORSConfig{AllowedOrigins: parseCSV("CORS_ALLOWED_ORIGINS")},
	}
	remoteServers, err := parseRemoteServers(os.Getenv("UI_REMOTE_SERVERS"))
	if err != nil {
		return Config{}, err
	}
	cfg.UI = UIConfig{
		APIBase:       strings.TrimRight(strings.TrimSpace(getString("UI_API_BASE", "")), "/"),
		RemoteServers: remoteServers,
	}
	mounts, err := parseMounts(os.Getenv("STORAGE_MOUNTS"), cfg.Storage.Root)
	if err != nil {
		return Config{}, err
	}
	cfg.Storage.Mounts = mounts
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// mountIDPattern is the allowed shape of a mount id; the id is used verbatim
// in URLs and in the database, so it must be a short, safe identifier.
var mountIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

// parseMounts reads the STORAGE_MOUNTS value ("Name1=/path1;Name2=/path2") and
// returns one MountSpec per entry. When the variable is unset or blank, a
// single default mount is derived from the legacy STORAGE_ROOT path so
// existing deployments keep working unchanged.
func parseMounts(value, root string) ([]storage.MountSpec, error) {
	if strings.TrimSpace(value) == "" {
		return []storage.MountSpec{{ID: "default", Name: "Main", Root: strings.TrimSpace(root)}}, nil
	}
	var mounts []storage.MountSpec
	for _, entry := range strings.Split(value, ";") {
		spec, err := parseMountEntry(entry)
		if err != nil {
			return nil, err
		}
		if spec.ID != "" {
			mounts = append(mounts, spec)
		}
	}
	if len(mounts) == 0 {
		return nil, fmt.Errorf("STORAGE_MOUNTS must name at least one disk")
	}
	// The first disk is the default disk and keeps the id "default" so files
	// stored before multi-disk support (which are tagged with "default")
	// remain visible on it. If the user already named a disk "default", that
	// name is respected instead of creating a duplicate id.
	hasDefault := false
	for _, mount := range mounts {
		if mount.ID == "default" {
			hasDefault = true
			break
		}
	}
	if !hasDefault {
		mounts[0].ID = "default"
	}
	return mounts, nil
}

func parseMountEntry(entry string) (storage.MountSpec, error) {
	entry = strings.TrimSpace(entry)
	if entry == "" {
		return storage.MountSpec{}, nil // tolerate a trailing separator
	}
	name, path, found := strings.Cut(entry, "=")
	name = strings.TrimSpace(name)
	path = strings.TrimSpace(path)
	if !found || !mountIDPattern.MatchString(name) {
		return storage.MountSpec{}, fmt.Errorf("STORAGE_MOUNTS entry %q is invalid: use Name=/absolute/path with a short alphanumeric name", entry)
	}
	if path == "" {
		return storage.MountSpec{}, fmt.Errorf("STORAGE_MOUNTS entry %q has an empty path", entry)
	}
	return storage.MountSpec{ID: name, Name: name, Root: path}, nil
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
	if c.Storage.MaxBytes <= 0 {
		return fmt.Errorf("storage configuration is invalid")
	}
	if len(c.Storage.Mounts) == 0 {
		return fmt.Errorf("at least one storage mount is required")
	}
	seen := make(map[string]struct{}, len(c.Storage.Mounts))
	for _, mount := range c.Storage.Mounts {
		if !mountIDPattern.MatchString(mount.ID) || strings.TrimSpace(mount.Name) == "" || strings.TrimSpace(mount.Root) == "" {
			return fmt.Errorf("storage mount %q is invalid", mount.ID)
		}
		if _, ok := seen[mount.ID]; ok {
			return fmt.Errorf("duplicate storage mount id %q", mount.ID)
		}
		seen[mount.ID] = struct{}{}
	}
	for _, origin := range c.CORS.AllowedOrigins {
		if !validCORSOrigin(origin) {
			return fmt.Errorf("CORS_ALLOWED_ORIGINS entry %q is invalid: use an absolute http(s) origin without a path (e.g. https://files.example.com)", origin)
		}
	}
	if strings.TrimSpace(c.UI.APIBase) != "" && !validOrigin(c.UI.APIBase) {
		return fmt.Errorf("UI_API_BASE must be an absolute http(s) URL")
	}
	seenServers := make(map[string]struct{}, len(c.UI.RemoteServers))
	seenBases := make(map[string]struct{}, len(c.UI.RemoteServers))
	for _, server := range c.UI.RemoteServers {
		if !mountIDPattern.MatchString(server.Name) || strings.TrimSpace(server.Base) == "" {
			return fmt.Errorf("UI remote server %q is invalid: use a short alphanumeric name and an absolute http(s) URL", server.Name)
		}
		if !validOrigin(server.Base) {
			return fmt.Errorf("UI_REMOTE_SERVERS entry %q has an invalid base URL", server.Name)
		}
		if _, ok := seenServers[server.Name]; ok {
			return fmt.Errorf("duplicate UI remote server name %q", server.Name)
		}
		seenServers[server.Name] = struct{}{}
		if _, ok := seenBases[server.Base]; ok {
			return fmt.Errorf("UI remote server %q duplicates the base URL of another server", server.Name)
		}
		seenBases[server.Base] = struct{}{}
	}
	return nil
}

// parseRemoteServers reads the UI_REMOTE_SERVERS value
// ("Name1=http://host:port;Name2=https://host2") into RemoteServer entries.
// A blank value yields no remote servers; entries use the same separator
// convention as STORAGE_MOUNTS.
func parseRemoteServers(value string) ([]RemoteServer, error) {
	var servers []RemoteServer
	for _, entry := range strings.Split(value, ";") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue // tolerate a trailing separator
		}
		name, base, found := strings.Cut(entry, "=")
		name = strings.TrimSpace(name)
		base = strings.TrimRight(strings.TrimSpace(base), "/")
		if !found || name == "" || !mountIDPattern.MatchString(name) || base == "" {
			return nil, fmt.Errorf("UI_REMOTE_SERVERS entry %q is invalid: use Name=http(s)://host[:port]", entry)
		}
		if !validOrigin(base) {
			return nil, fmt.Errorf("UI_REMOTE_SERVERS entry %q has an invalid base URL", entry)
		}
		servers = append(servers, RemoteServer{Name: name, Base: base})
	}
	return servers, nil
}

// validOrigin reports whether value is an absolute http(s) URL with a host.
func validOrigin(value string) bool {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil {
		return false
	}
	return (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
}

// validCORSOrigin is validOrigin plus a rule that the value must be a bare
// origin: a browser Origin header never contains a path or query string, so
// entries with one would silently never match.
func validCORSOrigin(value string) bool {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil {
		return false
	}
	return (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != "" && parsed.Path == "" && parsed.RawQuery == "" && parsed.Fragment == ""
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

// parseCSV splits a comma-separated env value into trimmed, non-empty items.
func parseCSV(key string) []string {
	var items []string
	for _, item := range strings.Split(os.Getenv(key), ",") {
		if item = strings.TrimSpace(item); item != "" {
			items = append(items, strings.TrimRight(item, "/"))
		}
	}
	return items
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
