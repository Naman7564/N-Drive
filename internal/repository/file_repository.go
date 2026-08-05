package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Folder is a directory.
type Folder struct {
	ID        string    `json:"id"`
	ParentID  string    `json:"parent_id,omitempty"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// File is a file metadata record.
type File struct {
	ID          string     `json:"id"`
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
		var exists bool
		if err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM folders WHERE id=?)`, folder.ParentID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return ErrNotFound
		}
	}
	_, err := r.db.ExecContext(ctx, `INSERT INTO folders (id,parent_id,name,created_at,updated_at) VALUES (?,?,?,?,?)`, folder.ID, nullableString(folder.ParentID), folder.Name, stamp(folder.CreatedAt), stamp(folder.UpdatedAt))
	if err != nil {
		if isConstraint(err) {
			return ErrConflict
		}
		return fmt.Errorf("create folder: %w", err)
	}
	return nil
}
func (r *FileRepository) ListFolders(ctx context.Context, parentID string, limit, offset int) ([]Folder, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,COALESCE(parent_id,''),name,created_at,updated_at FROM folders WHERE COALESCE(parent_id,'')=? ORDER BY name LIMIT ? OFFSET ?`, parentID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list folders: %w", err)
	}
	defer rows.Close()
	var result []Folder
	for rows.Next() {
		var item Folder
		var created, updated string
		if err := rows.Scan(&item.ID, &item.ParentID, &item.Name, &created, &updated); err != nil {
			return nil, err
		}
		item.CreatedAt = parseStamp(created)
		item.UpdatedAt = parseStamp(updated)
		result = append(result, item)
	}
	return result, rows.Err()
}
func (r *FileRepository) FindFolder(ctx context.Context, id string) (Folder, error) {
	var item Folder
	var created, updated string
	err := r.db.QueryRowContext(ctx, `SELECT id,COALESCE(parent_id,''),name,created_at,updated_at FROM folders WHERE id=?`, id).Scan(&item.ID, &item.ParentID, &item.Name, &created, &updated)
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
func (r *FileRepository) RenameFolder(ctx context.Context, id, name string, now time.Time) error {
	result, err := r.db.ExecContext(ctx, `UPDATE folders SET name=?,updated_at=? WHERE id=?`, name, stamp(now), id)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
func (r *FileRepository) DeleteFolder(ctx context.Context, id string) error {
	var children, files int
	if err := r.db.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM folders WHERE parent_id=?), (SELECT COUNT(*) FROM files WHERE folder_id=?)`, id, id).Scan(&children, &files); err != nil {
		return err
	}
	if children > 0 || files > 0 {
		return ErrConflict
	}
	result, err := r.db.ExecContext(ctx, `DELETE FROM folders WHERE id=?`, id)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *FileRepository) FindActiveByChecksum(ctx context.Context, checksum string) (File, error) {
	var item File
	var folder, created, updated string
	err := r.db.QueryRowContext(ctx, `SELECT id,COALESCE(folder_id,''),storage_key,name,content_type,size,checksum,created_at,updated_at FROM files WHERE checksum=? AND deleted_at IS NULL LIMIT 1`, checksum).Scan(&item.ID, &folder, &item.StorageKey, &item.Name, &item.ContentType, &item.Size, &item.Checksum, &created, &updated)
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
		var exists bool
		if err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM folders WHERE id=?)`, item.FolderID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return ErrNotFound
		}
	}
	_, err := r.db.ExecContext(ctx, `INSERT INTO files (id,folder_id,storage_key,name,content_type,size,checksum,created_at,updated_at,deleted_at) VALUES (?,?,?,?,?,?,?,?,?,NULL)`, item.ID, nullableString(item.FolderID), item.StorageKey, item.Name, item.ContentType, item.Size, item.Checksum, stamp(item.CreatedAt), stamp(item.UpdatedAt))
	if err != nil {
		if isConstraint(err) {
			return ErrConflict
		}
		return fmt.Errorf("create file: %w", err)
	}
	return nil
}
func (r *FileRepository) FindFile(ctx context.Context, id string) (File, error) {
	var item File
	var folder, created, updated, deleted sql.NullString
	err := r.db.QueryRowContext(ctx, `SELECT id,COALESCE(folder_id,''),storage_key,name,content_type,size,checksum,created_at,updated_at,deleted_at FROM files WHERE id=? AND deleted_at IS NULL`, id).Scan(&item.ID, &folder, &item.StorageKey, &item.Name, &item.ContentType, &item.Size, &item.Checksum, &created, &updated, &deleted)
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
func (r *FileRepository) ListFiles(ctx context.Context, folderID string, limit, offset int) ([]File, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,COALESCE(folder_id,''),storage_key,name,content_type,size,checksum,created_at,updated_at FROM files WHERE COALESCE(folder_id,'')=? AND deleted_at IS NULL ORDER BY name LIMIT ? OFFSET ?`, folderID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []File
	for rows.Next() {
		var item File
		var folder, created, updated string
		if err := rows.Scan(&item.ID, &folder, &item.StorageKey, &item.Name, &item.ContentType, &item.Size, &item.Checksum, &created, &updated); err != nil {
			return nil, err
		}
		item.FolderID = folder
		item.CreatedAt = parseStamp(created)
		item.UpdatedAt = parseStamp(updated)
		result = append(result, item)
	}
	return result, rows.Err()
}
func (r *FileRepository) SoftDeleteFile(ctx context.Context, id string, now time.Time) error {
	result, err := r.db.ExecContext(ctx, `UPDATE files SET deleted_at=?,updated_at=? WHERE id=? AND deleted_at IS NULL`, stamp(now), stamp(now), id)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
func (r *FileRepository) RestoreFile(ctx context.Context, id string) error {
	result, err := r.db.ExecContext(ctx, `UPDATE files SET deleted_at=NULL WHERE id=? AND deleted_at IS NOT NULL`, id)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
func (r *FileRepository) ListTrash(ctx context.Context, limit, offset int) ([]File, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,COALESCE(folder_id,''),storage_key,name,content_type,size,checksum,created_at,updated_at,deleted_at FROM files WHERE deleted_at IS NOT NULL ORDER BY deleted_at DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []File
	for rows.Next() {
		var item File
		var folder, created, updated, deleted string
		if err := rows.Scan(&item.ID, &folder, &item.StorageKey, &item.Name, &item.ContentType, &item.Size, &item.Checksum, &created, &updated, &deleted); err != nil {
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
func (r *FileRepository) FindTrashedFile(ctx context.Context, id string) (File, error) {
	var item File
	var folder, created, updated, deleted sql.NullString
	err := r.db.QueryRowContext(ctx, `SELECT id,COALESCE(folder_id,''),storage_key,name,content_type,size,checksum,created_at,updated_at,deleted_at FROM files WHERE id=? AND deleted_at IS NOT NULL`, id).Scan(&item.ID, &folder, &item.StorageKey, &item.Name, &item.ContentType, &item.Size, &item.Checksum, &created, &updated, &deleted)
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
func (r *FileRepository) DeleteFilePermanently(ctx context.Context, id string) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM files WHERE id=? AND deleted_at IS NOT NULL`, id)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
func (r *FileRepository) Search(ctx context.Context, query string, limit, offset int) ([]File, error) {
	like := "%" + strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(query) + "%"
	rows, err := r.db.QueryContext(ctx, `SELECT id,COALESCE(folder_id,''),storage_key,name,content_type,size,checksum,created_at,updated_at FROM files WHERE deleted_at IS NULL AND name LIKE ? ESCAPE '!' ORDER BY name LIMIT ? OFFSET ?`, like, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []File
	for rows.Next() {
		var item File
		var folder, created, updated string
		if err := rows.Scan(&item.ID, &folder, &item.StorageKey, &item.Name, &item.ContentType, &item.Size, &item.Checksum, &created, &updated); err != nil {
			return nil, err
		}
		item.FolderID = folder
		item.CreatedAt = parseStamp(created)
		item.UpdatedAt = parseStamp(updated)
		result = append(result, item)
	}
	return result, rows.Err()
}
func (r *FileRepository) Dashboard(ctx context.Context) (map[string]int64, error) {
	var files, folders, bytes, trash int64
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(size),0) FROM files WHERE deleted_at IS NULL`).Scan(&files, &bytes); err != nil {
		return nil, err
	}
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM folders`).Scan(&folders); err != nil {
		return nil, err
	}
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM files WHERE deleted_at IS NOT NULL`).Scan(&trash); err != nil {
		return nil, err
	}
	return map[string]int64{"files": files, "folders": folders, "bytes": bytes, "trash": trash}, nil
}
func (r *FileRepository) RenameFile(ctx context.Context, id, name string, now time.Time) error {
	result, err := r.db.ExecContext(ctx, `UPDATE files SET name=?,updated_at=? WHERE id=? AND deleted_at IS NULL`, name, stamp(now), id)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
func (r *FileRepository) MoveFile(ctx context.Context, id, folderID string, now time.Time) error {
	if folderID != "" {
		var exists bool
		if err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM folders WHERE id=?)`, folderID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return ErrNotFound
		}
	}
	result, err := r.db.ExecContext(ctx, `UPDATE files SET folder_id=?,updated_at=? WHERE id=? AND deleted_at IS NULL`, nullableString(folderID), stamp(now), id)
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
	_, err := r.db.ExecContext(ctx, `INSERT INTO audit_events (id,action,resource_id,metadata,created_at) VALUES (?,?,?,?,?)`, event.ID, event.Action, event.ResourceID, event.Metadata, stamp(event.CreatedAt))
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
