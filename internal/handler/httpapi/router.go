package httpapi

import (
	"context"
	"log/slog"
	"net/http"

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
	if cfg.Auth.JWTSecret == "" || cfg.Database.Path == "" || cfg.Storage.Root == "" || cfg.Storage.MaxBytes == 0 {
		loaded, err := config.Load()
		if err != nil {
			panic(err)
		}
		cfg = loaded
	}
	db, err := database.Open(ctx, cfg.Database.Path)
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
	store, err := storage.NewLocalStore(cfg.Storage.Root, cfg.Storage.MaxBytes, cfg.Storage.AllowedMIMEs)
	if err != nil {
		panic(err)
	}
	fileRepository := repository.NewFileRepository(db)
	fileService := service.NewFileService(fileRepository, store)
	fileHandler := &fileHandler{service: fileService, repo: fileRepository, store: store, serviceMaxBytes: cfg.Storage.MaxBytes}
	protected := func(handler http.Handler) http.Handler { return authMiddleware.RequireAccessToken(handler) }

	mux := http.NewServeMux()
	mux.HandleFunc("GET /", webHome)
	mux.HandleFunc("GET /health", health)
	mux.Handle("POST /api/auth/login", middleware.NewRateLimiter(cfg.Auth.LoginRateLimit, cfg.Auth.LoginRateWindow).Middleware(http.HandlerFunc(authHandler.login)))
	mux.Handle("POST /api/auth/refresh", middleware.NewRateLimiter(cfg.Auth.RefreshRateLimit, cfg.Auth.RefreshRateWindow).Middleware(http.HandlerFunc(authHandler.refresh)))
	mux.Handle("POST /api/auth/logout", protected(http.HandlerFunc(authHandler.logout)))
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
	mux.Handle("GET /api/dashboard", protected(http.HandlerFunc(fileHandler.dashboard)))

	return middleware.Chain(
		mux,
		middleware.Recover(logger),
		middleware.RequestID,
		middleware.SecurityHeaders,
		middleware.Logging(logger),
	), db.Close
}
