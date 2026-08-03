package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"fileservice/internal/auth"
)

var ErrNotFound = errors.New("not found")
var ErrConflict = errors.New("already exists")

// AuthRepository persists authentication identities and sessions.
type AuthRepository struct {
	db *sql.DB
}

// NewAuthRepository creates an authentication repository.
func NewAuthRepository(db *sql.DB) *AuthRepository { return &AuthRepository{db: db} }

// CreateUser stores a new user.
func (r *AuthRepository) CreateUser(ctx context.Context, user auth.User) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO users (id, email, password_hash, created_at) VALUES (?, ?, ?, ?)`, user.ID, user.Email, user.PasswordHash, user.CreatedAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		if isConstraint(err) {
			return ErrConflict
		}
		return fmt.Errorf("create user: %w", err)
	}
	return nil
}

// FindUserByEmail retrieves a user by normalized email.
func (r *AuthRepository) FindUserByEmail(ctx context.Context, email string) (auth.User, error) {
	var user auth.User
	var created string
	err := r.db.QueryRowContext(ctx, `SELECT id, email, password_hash, created_at FROM users WHERE email = ?`, email).Scan(&user.ID, &user.Email, &user.PasswordHash, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return auth.User{}, ErrNotFound
	}
	if err != nil {
		return auth.User{}, fmt.Errorf("find user: %w", err)
	}
	user.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
	if err != nil {
		return auth.User{}, fmt.Errorf("parse user creation time: %w", err)
	}
	return user, nil
}

// CreateUserAndSession atomically stores a user and its initial session.
func (r *AuthRepository) CreateUserAndSession(ctx context.Context, user auth.User, session auth.Session) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin create user transaction: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO users (id, email, password_hash, created_at) VALUES (?, ?, ?, ?)`, user.ID, user.Email, user.PasswordHash, user.CreatedAt.UTC().Format(time.RFC3339Nano)); err != nil {
		if isConstraint(err) {
			return ErrConflict
		}
		return fmt.Errorf("create user: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO sessions (id, user_id, token_hash, expires_at, revoked_at, created_at) VALUES (?, ?, ?, ?, ?, ?)`, session.ID, session.UserID, session.TokenHash, session.ExpiresAt.UTC().Format(time.RFC3339Nano), nullableTime(session.RevokedAt), session.CreatedAt.UTC().Format(time.RFC3339Nano)); err != nil {
		return fmt.Errorf("create initial session: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit user transaction: %w", err)
	}
	return nil
}

// CreateSession stores a refresh-token session.
func (r *AuthRepository) CreateSession(ctx context.Context, session auth.Session) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO sessions (id, user_id, token_hash, expires_at, revoked_at, created_at) VALUES (?, ?, ?, ?, ?, ?)`, session.ID, session.UserID, session.TokenHash, session.ExpiresAt.UTC().Format(time.RFC3339Nano), nullableTime(session.RevokedAt), session.CreatedAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

// FindActiveSessionByTokenHash retrieves a non-expired, non-revoked session.
func (r *AuthRepository) FindActiveSessionByTokenHash(ctx context.Context, tokenHash string, now time.Time) (auth.Session, error) {
	var session auth.Session
	var expires, revoked, created sql.NullString
	err := r.db.QueryRowContext(ctx, `SELECT id, user_id, token_hash, expires_at, revoked_at, created_at FROM sessions WHERE token_hash = ?`, tokenHash).Scan(&session.ID, &session.UserID, &session.TokenHash, &expires, &revoked, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return auth.Session{}, ErrNotFound
	}
	if err != nil {
		return auth.Session{}, fmt.Errorf("find session: %w", err)
	}
	var parseErr error
	session.ExpiresAt, parseErr = time.Parse(time.RFC3339Nano, expires.String)
	if parseErr != nil {
		return auth.Session{}, fmt.Errorf("parse session expiry: %w", parseErr)
	}
	session.CreatedAt, parseErr = time.Parse(time.RFC3339Nano, created.String)
	if parseErr != nil {
		return auth.Session{}, fmt.Errorf("parse session creation: %w", parseErr)
	}
	if revoked.Valid {
		value, err := time.Parse(time.RFC3339Nano, revoked.String)
		if err != nil {
			return auth.Session{}, fmt.Errorf("parse session revocation: %w", err)
		}
		session.RevokedAt = &value
	}
	if session.RevokedAt != nil || !session.ExpiresAt.After(now) {
		return auth.Session{}, auth.ErrSessionRevoked
	}
	return session, nil
}

// ValidateSession confirms that an access-token session remains active.
func (r *AuthRepository) ValidateSession(ctx context.Context, sessionID, userID string, now time.Time) error {
	var expires string
	var revoked sql.NullString
	err := r.db.QueryRowContext(ctx, `SELECT expires_at, revoked_at FROM sessions WHERE id = ? AND user_id = ?`, sessionID, userID).Scan(&expires, &revoked)
	if errors.Is(err, sql.ErrNoRows) {
		return auth.ErrSessionRevoked
	}
	if err != nil {
		return fmt.Errorf("validate session: %w", err)
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, expires)
	if err != nil {
		return fmt.Errorf("parse session expiry: %w", err)
	}
	if revoked.Valid || !expiresAt.After(now) {
		return auth.ErrSessionRevoked
	}
	return nil
}

// RotateSession atomically revokes an old session and creates its replacement.
func (r *AuthRepository) RotateSession(ctx context.Context, oldSessionID string, replacement auth.Session, now time.Time) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin rotate session transaction: %w", err)
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE sessions SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL AND expires_at > ?`, now.UTC().Format(time.RFC3339Nano), oldSessionID, now.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("revoke old session: %w", err)
	}
	count, _ := result.RowsAffected()
	if count != 1 {
		return auth.ErrSessionRevoked
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO sessions (id, user_id, token_hash, expires_at, revoked_at, created_at) VALUES (?, ?, ?, ?, ?, ?)`, replacement.ID, replacement.UserID, replacement.TokenHash, replacement.ExpiresAt.UTC().Format(time.RFC3339Nano), nullableTime(replacement.RevokedAt), replacement.CreatedAt.UTC().Format(time.RFC3339Nano)); err != nil {
		return fmt.Errorf("create rotated session: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit rotate session transaction: %w", err)
	}
	return nil
}

// RevokeSession atomically invalidates a refresh session.
func (r *AuthRepository) RevokeSession(ctx context.Context, sessionID string, now time.Time) error {
	result, err := r.db.ExecContext(ctx, `UPDATE sessions SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL`, now.UTC().Format(time.RFC3339Nano), sessionID)
	if err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return ErrNotFound
	}
	return nil
}

func nullableTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func isConstraint(err error) bool {
	return err != nil && (contains(err.Error(), "UNIQUE") || contains(err.Error(), "constraint"))
}

func contains(value, part string) bool {
	return len(value) >= len(part) && stringContainsFold(value, part)
}

func stringContainsFold(value, part string) bool {
	for i := 0; i+len(part) <= len(value); i++ {
		match := true
		for j := range part {
			if lower(value[i+j]) != lower(part[j]) {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func lower(value byte) byte {
	if value >= 'A' && value <= 'Z' {
		return value + ('a' - 'A')
	}
	return value
}
