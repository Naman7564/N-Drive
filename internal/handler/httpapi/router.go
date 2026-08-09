package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"fileservice/internal/auth"
	"fileservice/internal/config"
	"fileservice/internal/database"
	"fileservice/internal/middleware"
	"fileservice/internal/repository"
	"fileservice/internal/service"
	"fileservice/internal/storage"
)

// NewRouterWithCloser constructs the API and returns a handler plus its database closer.
func NewRouterWithCloser(ctx context.Context, logger *slog.Logger, cfg config.Config) (http.Handler, func() error) {
	if cfg.Auth.JWTSecret == "" || cfg.Auth.Username == "" || cfg.Auth.Password == "" || cfg.Database.Path == "" || (cfg.Storage.Root == "" && len(cfg.Storage.Mounts) == 0) || cfg.Storage.MaxBytes == 0 {
		loaded, err := config.Load()
		if err != nil {
			panic(err)
		}
		cfg = loaded
	}
	db, err := database.Open(ctx, cfg.Database.Path, database.SeedCredentials{Username: cfg.Auth.Username, Password: cfg.Auth.Password})
	if err != nil {
		panic(err)
	}
	tokens, err := auth.NewTokenManager(cfg.Auth.JWTSecret, cfg.Auth.JWTIssuer, cfg.Auth.AccessTokenTTL, cfg.Auth.RefreshTokenTTL)
	if err != nil {
		panic(err)
	}
	authRepository := repository.NewAuthRepository(db)
	authService := service.NewAuthService(authRepository, tokens)
	authHandler := &authHandler{service: authService, secureCookies: cfg.Auth.SecureCookies, cookieDomain: cfg.Auth.CookieDomain}
	authMiddleware := middleware.Auth{Tokens: tokens, Sessions: authRepository}
	mountSpecs := cfg.Storage.Mounts
	if len(mountSpecs) == 0 {
		// Callers that construct the config directly (tests) may omit the
		// mounts; derive a single default disk from the storage root.
		mountSpecs = []storage.MountSpec{{ID: "default", Name: "Main", Root: cfg.Storage.Root}}
	}
	mounts, err := storage.NewMounts(mountSpecs, cfg.Storage.MaxBytes, cfg.Storage.AllowedMIMEs)
	if err != nil {
		panic(err)
	}
	fileRepository := repository.NewFileRepository(db)
	// Prune housekeeping data at startup: expired sessions and old audit events.
	// Failures are non-fatal; pruning resumes on the next boot.
	if _, err := authRepository.PruneSessions(ctx, time.Now().UTC()); err != nil {
		logger.Warn("prune sessions", "error", err)
	}
	if _, err := fileRepository.PruneAudit(ctx, time.Now().UTC()); err != nil {
		logger.Warn("prune audit events", "error", err)
	}
	fileService := service.NewFileService(fileRepository, mounts)
	fileHandler := &fileHandler{service: fileService, repo: fileRepository, mounts: mounts, serviceMaxBytes: cfg.Storage.MaxBytes}
	protected := func(handler http.Handler) http.Handler { return authMiddleware.RequireAccessToken(handler) }

	mux := http.NewServeMux()
	// UI_DISABLED runs an API-only backend: the web pages are not registered
	// at all, so "/", "/app", and "/landing" fall through to a 404 while
	// "/health" and the "/api/*" routes below keep working.
	if !cfg.UI.Disabled {
		mux.HandleFunc("GET /", webLanding)
		mux.HandleFunc("GET /app", webHomeWith(cfg.UI.APIBase, cfg.UI.RemoteServers))
		mux.HandleFunc("GET /landing", webLanding)
	}
	mux.HandleFunc("GET /health", health)
	mux.Handle("POST /api/auth/login", middleware.NewRateLimiter(cfg.Auth.LoginRateLimit, cfg.Auth.LoginRateWindow).Middleware(http.HandlerFunc(authHandler.login)))
	mux.Handle("POST /api/auth/refresh", middleware.NewRateLimiter(cfg.Auth.RefreshRateLimit, cfg.Auth.RefreshRateWindow).Middleware(http.HandlerFunc(authHandler.refresh)))
	mux.Handle("POST /api/auth/logout", protected(http.HandlerFunc(authHandler.logout)))
	mux.Handle("GET /api/auth/me", protected(http.HandlerFunc(authHandler.me)))
	mux.Handle("GET /api/folders", protected(http.HandlerFunc(fileHandler.listFolders)))
	mux.Handle("POST /api/folders", protected(http.HandlerFunc(fileHandler.createFolder)))
	mux.Handle("PATCH /api/folders/{id}", protected(http.HandlerFunc(fileHandler.renameFolder)))
	mux.Handle("DELETE /api/folders/{id}", protected(http.HandlerFunc(fileHandler.deleteFolder)))
	mux.Handle("GET /api/files", protected(http.HandlerFunc(fileHandler.listFiles)))
	mux.Handle("POST /api/files/upload", protected(http.HandlerFunc(fileHandler.upload)))
	mux.Handle("GET /api/files/{id}/download", protected(http.HandlerFunc(fileHandler.download)))
	mux.Handle("PATCH /api/files/{id}", protected(http.HandlerFunc(fileHandler.renameFile)))
	mux.Handle("POST /api/files/{id}/copy", protected(http.HandlerFunc(fileHandler.copyFile)))
	mux.Handle("POST /api/files/{id}/move", protected(http.HandlerFunc(fileHandler.moveFile)))
	mux.Handle("DELETE /api/files/{id}", protected(http.HandlerFunc(fileHandler.deleteFile)))
	mux.Handle("GET /api/trash", protected(http.HandlerFunc(fileHandler.trash)))
	mux.Handle("POST /api/trash/{id}/restore", protected(http.HandlerFunc(fileHandler.restore)))
	mux.Handle("DELETE /api/trash/{id}", protected(http.HandlerFunc(fileHandler.purge)))
	mux.Handle("GET /api/search", protected(http.HandlerFunc(fileHandler.search)))
	mux.Handle("GET /api/disks", protected(http.HandlerFunc(fileHandler.disks)))
	mux.Handle("GET /api/dashboard", protected(http.HandlerFunc(fileHandler.dashboard)))

	return middleware.Chain(
		mux,
		middleware.NewCORS(cfg.CORS.AllowedOrigins).Middleware,
		middleware.Recover(logger),
		middleware.RequestID,
		middleware.SecurityHeaders,
		middleware.Logging(logger),
	), db.Close
}
