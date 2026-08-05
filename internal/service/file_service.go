package service

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"

	"fileservice/internal/repository"
	"fileservice/internal/storage"
)

// FileService coordinates metadata persistence and object storage.
type FileService struct {
	repo  *repository.FileRepository
	store *storage.LocalStore
	now   func() time.Time
}

// NewFileService creates a file service.
func NewFileService(repo *repository.FileRepository, store *storage.LocalStore) *FileService {
	return &FileService{repo: repo, store: store, now: time.Now}
}

// CreateFolder creates a folder.
func (s *FileService) CreateFolder(ctx context.Context, parentID, name string) (repository.Folder, error) {
	if name == "" || len(name) > 255 || strings.ContainsAny(name, "\\r\\n") {
		return repository.Folder{}, fmt.Errorf("folder name is invalid")
	}
	now := s.now().UTC()
	item := repository.Folder{ID: uuid.NewString(), ParentID: parentID, Name: name, CreatedAt: now, UpdatedAt: now}
	if err := s.repo.CreateFolder(ctx, item); err != nil {
		return repository.Folder{}, err
	}
	return item, nil
}

// Upload stores an object and its metadata.
func (s *FileService) Upload(ctx context.Context, folderID, filename, mime string, src io.Reader) (repository.File, error) {
	stored, err := s.store.Save(src, filename, mime)
	if err != nil {
		return repository.File{}, err
	}
	now := s.now().UTC()
	if existing, lookupErr := s.repo.FindActiveByChecksum(ctx, stored.Checksum); lookupErr == nil && existing.ID != "" {
		_ = s.store.Delete(stored.Key)
		return existing, nil
	}
	item := repository.File{ID: uuid.NewString(), FolderID: folderID, StorageKey: stored.Key, Name: stored.Filename, ContentType: stored.ContentType, Size: stored.Size, Checksum: stored.Checksum, CreatedAt: now, UpdatedAt: now}
	if err := s.repo.CreateFile(ctx, item); err != nil {
		_ = s.store.Delete(stored.Key)
		return repository.File{}, err
	}
	_ = s.repo.Audit(ctx, repository.AuditEvent{ID: uuid.NewString(), Action: "upload", ResourceID: item.ID, Metadata: item.Name, CreatedAt: now})
	return item, nil
}

// DeleteFile moves a file to trash.
func (s *FileService) DeleteFile(ctx context.Context, id string) error {
	if err := s.repo.SoftDeleteFile(ctx, id, s.now().UTC()); err != nil {
		return err
	}
	_ = s.repo.Audit(ctx, repository.AuditEvent{ID: uuid.NewString(), Action: "delete", ResourceID: id, CreatedAt: s.now().UTC()})
	return nil
}

// RenameFolder changes a folder display name.
func (s *FileService) RenameFolder(ctx context.Context, id, name string) error {
	if name == "" || len(name) > 255 || strings.ContainsAny(name, "\\r\\n") {
		return fmt.Errorf("folder name is invalid")
	}
	return s.repo.RenameFolder(ctx, id, name, s.now().UTC())
}

// RestoreFile restores a trashed file.
func (s *FileService) RestoreFile(ctx context.Context, id string) error {
	if err := s.repo.RestoreFile(ctx, id); err != nil {
		return err
	}
	_ = s.repo.Audit(ctx, repository.AuditEvent{ID: uuid.NewString(), Action: "restore", ResourceID: id, CreatedAt: s.now().UTC()})
	return nil
}

// RenameFile changes a display name.
func (s *FileService) RenameFile(ctx context.Context, id, name string) error {
	cleanName, err := storage.SanitizeFilename(name)
	if err != nil {
		return fmt.Errorf("file name is invalid")
	}
	return s.repo.RenameFile(ctx, id, cleanName, s.now().UTC())
}

// MoveFile changes a file's parent folder.
func (s *FileService) MoveFile(ctx context.Context, id, folderID string) error {
	return s.repo.MoveFile(ctx, id, folderID, s.now().UTC())
}

// CopyFile duplicates metadata while reusing the immutable stored object.
func (s *FileService) CopyFile(ctx context.Context, id, folderID string) (repository.File, error) {
	source, err := s.repo.FindFile(ctx, id)
	if err != nil {
		return repository.File{}, err
	}
	stored, err := s.store.Copy(source.StorageKey, source.Name, source.ContentType)
	if err != nil {
		return repository.File{}, err
	}
	now := s.now().UTC()
	copy := source
	copy.ID = uuid.NewString()
	copy.FolderID = folderID
	copy.StorageKey = stored.Key
	copy.CreatedAt = now
	copy.UpdatedAt = now
	if err := s.repo.CreateFile(ctx, copy); err != nil {
		_ = s.store.Delete(stored.Key)
		return repository.File{}, err
	}
	return copy, nil
}

// PermanentlyDelete removes metadata and object data.
func (s *FileService) PermanentlyDelete(ctx context.Context, id string) error {
	item, err := s.repo.FindTrashedFile(ctx, id)
	if err != nil {
		return err
	}
	if err := s.repo.DeleteFilePermanently(ctx, id); err != nil {
		return err
	}
	if err := s.store.Delete(item.StorageKey); err != nil {
		return err
	}
	_ = s.repo.Audit(ctx, repository.AuditEvent{ID: uuid.NewString(), Action: "purge", ResourceID: id, CreatedAt: s.now().UTC()})
	return nil
}
