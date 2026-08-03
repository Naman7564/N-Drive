package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Folder is a user-owned directory.
type Folder struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	ParentID  string    `json:"parent_id,omitempty"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// File is a user-owned file metadata record.
type File struct {
	ID          string     `json:"id"`
	UserID      string     `json:"user_id"`
	FolderID    string     `json:"folder_id,omitempty"`
	StorageKey  string     `json:"-"`
	Name        string     `json:"name"`
	ContentType string     `json:"content_type"`
	Size        int64      `json:"size"`
	Checksum    string     `json:"checksum"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	DeletedAt   *time.Time `json:"deleted_at,omitempty"`
}

// AuditEvent records a security-relevant action.
type AuditEvent struct {
	ID         string    `json:"id"`
	UserID     string    `json:"user_id"`
	Action     string    `json:"action"`
	ResourceID string    `json:"resource_id"`
	Metadata   string    `json:"metadata"`
	CreatedAt  time.Time `json:"created_at"`
}

// FileRepository persists file-manager data.
type FileRepository struct{ db *sql.DB }

// NewFileRepository creates a file repository.
func NewFileRepository(db *sql.DB) *FileRepository { return &FileRepository{db: db} }

func (r *FileRepository) CreateFolder(ctx context.Context, folder Folder) error {
	if folder.ParentID != "" {
		var owner string
		if err := r.db.QueryRowContext(ctx, `SELECT user_id FROM folders WHERE id=?`, folder.ParentID).Scan(&owner); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		if owner != folder.UserID {
			return ErrNotFound
		}
	}
	_, err := r.db.ExecContext(ctx, `INSERT INTO folders (id,user_id,parent_id,name,created_at,updated_at) VALUES (?,?,?,?,?,?)`, folder.ID, folder.UserID, nullableString(folder.ParentID), folder.Name, stamp(folder.CreatedAt), stamp(folder.UpdatedAt))
	if err != nil {
		if isConstraint(err) {
			return ErrConflict
		}
		return fmt.Errorf("create folder: %w", err)
	}
	return nil
}
func (r *FileRepository) ListFolders(ctx context.Context, userID, parentID string, limit, offset int) ([]Folder, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,user_id,COALESCE(parent_id,''),name,created_at,updated_at FROM folders WHERE user_id=? AND COALESCE(parent_id,'')=? ORDER BY name LIMIT ? OFFSET ?`, userID, parentID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list folders: %w", err)
	}
	defer rows.Close()
	var result []Folder
	for rows.Next() {
		var item Folder
		var created, updated string
		if err := rows.Scan(&item.ID, &item.UserID, &item.ParentID, &item.Name, &created, &updated); err != nil {
			return nil, err
		}
		item.CreatedAt = parseStamp(created)
		item.UpdatedAt = parseStamp(updated)
		result = append(result, item)
	}
	return result, rows.Err()
}
func (r *FileRepository) FindFolder(ctx context.Context, userID, id string) (Folder, error) {
	var item Folder
	var created, updated string
	err := r.db.QueryRowContext(ctx, `SELECT id,user_id,COALESCE(parent_id,''),name,created_at,updated_at FROM folders WHERE user_id=? AND id=?`, userID, id).Scan(&item.ID, &item.UserID, &item.ParentID, &item.Name, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return Folder{}, ErrNotFound
	}
	if err != nil {
		return Folder{}, fmt.Errorf("find folder: %w", err)
	}
	item.CreatedAt = parseStamp(created)
	item.UpdatedAt = parseStamp(updated)
	return item, nil
}
func (r *FileRepository) RenameFolder(ctx context.Context, userID, id, name string, now time.Time) error {
	result, err := r.db.ExecContext(ctx, `UPDATE folders SET name=?,updated_at=? WHERE user_id=? AND id=?`, name, stamp(now), userID, id)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
func (r *FileRepository) DeleteFolder(ctx context.Context, userID, id string) error {
	var children, files int
	if err := r.db.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM folders WHERE user_id=? AND parent_id=?), (SELECT COUNT(*) FROM files WHERE user_id=? AND folder_id=?)`, userID, id, userID, id).Scan(&children, &files); err != nil {
		return err
	}
	if children > 0 || files > 0 {
		return ErrConflict
	}
	result, err := r.db.ExecContext(ctx, `DELETE FROM folders WHERE user_id=? AND id=?`, userID, id)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *FileRepository) FindActiveByChecksum(ctx context.Context, userID, checksum string) (File, error) {
	var item File
	var folder, created, updated string
	err := r.db.QueryRowContext(ctx, `SELECT id,user_id,COALESCE(folder_id,''),storage_key,name,content_type,size,checksum,created_at,updated_at FROM files WHERE user_id=? AND checksum=? AND deleted_at IS NULL LIMIT 1`, userID, checksum).Scan(&item.ID, &item.UserID, &folder, &item.StorageKey, &item.Name, &item.ContentType, &item.Size, &item.Checksum, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return File{}, ErrNotFound
	}
	if err != nil {
		return File{}, err
	}
	item.FolderID = folder
	item.CreatedAt = parseStamp(created)
	item.UpdatedAt = parseStamp(updated)
	return item, nil
}
func (r *FileRepository) CreateFile(ctx context.Context, item File) error {
	if item.FolderID != "" {
		var owner string
		if err := r.db.QueryRowContext(ctx, `SELECT user_id FROM folders WHERE id=?`, item.FolderID).Scan(&owner); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		if owner != item.UserID {
			return ErrNotFound
		}
	}
	_, err := r.db.ExecContext(ctx, `INSERT INTO files (id,user_id,folder_id,storage_key,name,content_type,size,checksum,created_at,updated_at,deleted_at) VALUES (?,?,?,?,?,?,?,?,?,?,NULL)`, item.ID, item.UserID, nullableString(item.FolderID), item.StorageKey, item.Name, item.ContentType, item.Size, item.Checksum, stamp(item.CreatedAt), stamp(item.UpdatedAt))
	if err != nil {
		if isConstraint(err) {
			return ErrConflict
		}
		return fmt.Errorf("create file: %w", err)
	}
	return nil
}
func (r *FileRepository) FindFile(ctx context.Context, userID, id string) (File, error) {
	var item File
	var folder, created, updated, deleted sql.NullString
	err := r.db.QueryRowContext(ctx, `SELECT id,user_id,COALESCE(folder_id,''),storage_key,name,content_type,size,checksum,created_at,updated_at,deleted_at FROM files WHERE user_id=? AND id=? AND deleted_at IS NULL`, userID, id).Scan(&item.ID, &item.UserID, &folder, &item.StorageKey, &item.Name, &item.ContentType, &item.Size, &item.Checksum, &created, &updated, &deleted)
	if errors.Is(err, sql.ErrNoRows) {
		return File{}, ErrNotFound
	}
	if err != nil {
		return File{}, err
	}
	item.FolderID = folder.String
	item.CreatedAt = parseStamp(created.String)
	item.UpdatedAt = parseStamp(updated.String)
	if deleted.Valid {
		v := parseStamp(deleted.String)
		item.DeletedAt = &v
	}
	return item, nil
}
func (r *FileRepository) ListFiles(ctx context.Context, userID, folderID string, limit, offset int) ([]File, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,user_id,COALESCE(folder_id,''),storage_key,name,content_type,size,checksum,created_at,updated_at FROM files WHERE user_id=? AND COALESCE(folder_id,'')=? AND deleted_at IS NULL ORDER BY name LIMIT ? OFFSET ?`, userID, folderID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []File
	for rows.Next() {
		var item File
		var folder, created, updated string
		if err := rows.Scan(&item.ID, &item.UserID, &folder, &item.StorageKey, &item.Name, &item.ContentType, &item.Size, &item.Checksum, &created, &updated); err != nil {
			return nil, err
		}
		item.FolderID = folder
		item.CreatedAt = parseStamp(created)
		item.UpdatedAt = parseStamp(updated)
		result = append(result, item)
	}
	return result, rows.Err()
}
func (r *FileRepository) SoftDeleteFile(ctx context.Context, userID, id string, now time.Time) error {
	result, err := r.db.ExecContext(ctx, `UPDATE files SET deleted_at=?,updated_at=? WHERE user_id=? AND id=? AND deleted_at IS NULL`, stamp(now), stamp(now), userID, id)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
func (r *FileRepository) RestoreFile(ctx context.Context, userID, id string) error {
	result, err := r.db.ExecContext(ctx, `UPDATE files SET deleted_at=NULL WHERE user_id=? AND id=? AND deleted_at IS NOT NULL`, userID, id)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
func (r *FileRepository) ListTrash(ctx context.Context, userID string, limit, offset int) ([]File, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,user_id,COALESCE(folder_id,''),storage_key,name,content_type,size,checksum,created_at,updated_at,deleted_at FROM files WHERE user_id=? AND deleted_at IS NOT NULL ORDER BY deleted_at DESC LIMIT ? OFFSET ?`, userID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []File
	for rows.Next() {
		var item File
		var folder, created, updated, deleted string
		if err := rows.Scan(&item.ID, &item.UserID, &folder, &item.StorageKey, &item.Name, &item.ContentType, &item.Size, &item.Checksum, &created, &updated, &deleted); err != nil {
			return nil, err
		}
		item.FolderID = folder
		item.CreatedAt = parseStamp(created)
		item.UpdatedAt = parseStamp(updated)
		v := parseStamp(deleted)
		item.DeletedAt = &v
		result = append(result, item)
	}
	return result, rows.Err()
}
func (r *FileRepository) FindTrashedFile(ctx context.Context, userID, id string) (File, error) {
	var item File
	var folder, created, updated, deleted sql.NullString
	err := r.db.QueryRowContext(ctx, `SELECT id,user_id,COALESCE(folder_id,''),storage_key,name,content_type,size,checksum,created_at,updated_at,deleted_at FROM files WHERE user_id=? AND id=? AND deleted_at IS NOT NULL`, userID, id).Scan(&item.ID, &item.UserID, &folder, &item.StorageKey, &item.Name, &item.ContentType, &item.Size, &item.Checksum, &created, &updated, &deleted)
	if errors.Is(err, sql.ErrNoRows) {
		return File{}, ErrNotFound
	}
	if err != nil {
		return File{}, err
	}
	item.FolderID = folder.String
	item.CreatedAt = parseStamp(created.String)
	item.UpdatedAt = parseStamp(updated.String)
	value := parseStamp(deleted.String)
	item.DeletedAt = &value
	return item, nil
}
func (r *FileRepository) DeleteFilePermanently(ctx context.Context, userID, id string) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM files WHERE user_id=? AND id=? AND deleted_at IS NOT NULL`, userID, id)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
func (r *FileRepository) Search(ctx context.Context, userID, query string, limit, offset int) ([]File, error) {
	like := "%" + strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(query) + "%"
	rows, err := r.db.QueryContext(ctx, `SELECT id,user_id,COALESCE(folder_id,''),storage_key,name,content_type,size,checksum,created_at,updated_at FROM files WHERE user_id=? AND deleted_at IS NULL AND name LIKE ? ESCAPE '!' ORDER BY name LIMIT ? OFFSET ?`, userID, like, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []File
	for rows.Next() {
		var item File
		var folder, created, updated string
		if err := rows.Scan(&item.ID, &item.UserID, &folder, &item.StorageKey, &item.Name, &item.ContentType, &item.Size, &item.Checksum, &created, &updated); err != nil {
			return nil, err
		}
		item.FolderID = folder
		item.CreatedAt = parseStamp(created)
		item.UpdatedAt = parseStamp(updated)
		result = append(result, item)
	}
	return result, rows.Err()
}
func (r *FileRepository) Dashboard(ctx context.Context, userID string) (map[string]int64, error) {
	var files, folders, bytes, trash int64
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(size),0) FROM files WHERE user_id=? AND deleted_at IS NULL`, userID).Scan(&files, &bytes)
	if err != nil {
		return nil, err
	}
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM folders WHERE user_id=?`, userID).Scan(&folders); err != nil {
		return nil, err
	}
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM files WHERE user_id=? AND deleted_at IS NOT NULL`, userID).Scan(&trash); err != nil {
		return nil, err
	}
	return map[string]int64{"files": files, "folders": folders, "bytes": bytes, "trash": trash}, nil
}
func (r *FileRepository) RenameFile(ctx context.Context, userID, id, name string, now time.Time) error {
	result, err := r.db.ExecContext(ctx, `UPDATE files SET name=?,updated_at=? WHERE user_id=? AND id=? AND deleted_at IS NULL`, name, stamp(now), userID, id)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
func (r *FileRepository) MoveFile(ctx context.Context, userID, id, folderID string, now time.Time) error {
	if folderID != "" {
		var owner string
		if err := r.db.QueryRowContext(ctx, `SELECT user_id FROM folders WHERE id=?`, folderID).Scan(&owner); err != nil {
			return ErrNotFound
		}
		if owner != userID {
			return ErrNotFound
		}
	}
	result, err := r.db.ExecContext(ctx, `UPDATE files SET folder_id=?,updated_at=? WHERE user_id=? AND id=? AND deleted_at IS NULL`, nullableString(folderID), stamp(now), userID, id)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
func (r *FileRepository) Audit(ctx context.Context, event AuditEvent) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO audit_events (id,user_id,action,resource_id,metadata,created_at) VALUES (?,?,?,?,?,?)`, event.ID, event.UserID, event.Action, event.ResourceID, event.Metadata, stamp(event.CreatedAt))
	return err
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
func stamp(value time.Time) string { return value.UTC().Format(time.RFC3339Nano) }
func parseStamp(value string) time.Time {
	parsed, _ := time.Parse(time.RFC3339Nano, value)
	return parsed
}
