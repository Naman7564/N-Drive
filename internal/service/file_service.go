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
	repo   *repository.FileRepository
	mounts *storage.Mounts
	now    func() time.Time
}

// NewFileService creates a file service backed by the configured disks.
func NewFileService(repo *repository.FileRepository, mounts *storage.Mounts) *FileService {
	return &FileService{repo: repo, mounts: mounts, now: time.Now}
}

// CreateFolder creates a folder. When a parent is given, the folder joins the
// parent's disk; otherwise it is created on the mount named by mountID.
func (s *FileService) CreateFolder(ctx context.Context, mountID, parentID, name string) (repository.Folder, error) {
	name = strings.TrimSpace(name)
	if err := validateFolderName(name); err != nil {
		return repository.Folder{}, err
	}
	if parentID != "" {
		parent, err := s.repo.FindFolder(ctx, parentID)
		if err != nil {
			return repository.Folder{}, err
		}
		mountID = parent.Mount
	}
	if _, err := s.mounts.Get(mountID); err != nil {
		return repository.Folder{}, err
	}
	now := s.now().UTC()
	item := repository.Folder{ID: uuid.NewString(), ParentID: parentID, Mount: mountID, Name: name, CreatedAt: now, UpdatedAt: now}
	if err := s.repo.CreateFolder(ctx, item); err != nil {
		return repository.Folder{}, err
	}
	return item, nil
}

// Upload stores an object and its metadata. Files inside a folder always land
// on that folder's disk; files at the root land on the mount named by mountID.
// Duplicate detection is scoped to the disk, so the same content can exist on
// different disks without sharing objects across mounts.
func (s *FileService) Upload(ctx context.Context, mountID, folderID, filename, mime string, src io.Reader) (repository.File, error) {
	if folderID != "" {
		folder, err := s.repo.FindFolder(ctx, folderID)
		if err != nil {
			return repository.File{}, err
		}
		mountID = folder.Mount
	}
	mount, err := s.mounts.Get(mountID)
	if err != nil {
		return repository.File{}, err
	}
	stored, err := mount.Store.Save(src, filename, mime)
	if err != nil {
		return repository.File{}, err
	}
	now := s.now().UTC()
	if existing, lookupErr := s.repo.FindActiveByChecksum(ctx, mount.ID, stored.Checksum); lookupErr == nil && existing.ID != "" {
		_ = mount.Store.Delete(stored.Key)
		return existing, nil
	}
	item := repository.File{ID: uuid.NewString(), FolderID: folderID, Mount: mount.ID, StorageKey: stored.Key, Name: stored.Filename, ContentType: stored.ContentType, Size: stored.Size, Checksum: stored.Checksum, CreatedAt: now, UpdatedAt: now}
	if err := s.repo.CreateFile(ctx, item); err != nil {
		_ = mount.Store.Delete(stored.Key)
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
	name = strings.TrimSpace(name)
	if err := validateFolderName(name); err != nil {
		return err
	}
	return s.repo.RenameFolder(ctx, id, name, s.now().UTC())
}

func validateFolderName(name string) error {
	if name == "" || len(name) > 255 {
		return fmt.Errorf("folder name is invalid")
	}
	if strings.ContainsAny(name, "\r\n/\\\x00") {
		return fmt.Errorf("folder name is invalid")
	}
	return nil
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

// MoveFile changes a file's parent folder. Moving between disks is rejected
// because each disk is an independent store.
func (s *FileService) MoveFile(ctx context.Context, id, folderID string) error {
	if folderID != "" {
		source, err := s.repo.FindFile(ctx, id)
		if err != nil {
			return err
		}
		folder, err := s.repo.FindFolder(ctx, folderID)
		if err != nil {
			return err
		}
		if source.Mount != folder.Mount {
			return repository.ErrMountMismatch
		}
	}
	return s.repo.MoveFile(ctx, id, folderID, s.now().UTC())
}

// CopyFile duplicates metadata while reusing the immutable stored object. The
// copy stays on the source disk.
func (s *FileService) CopyFile(ctx context.Context, id, folderID string) (repository.File, error) {
	source, err := s.repo.FindFile(ctx, id)
	if err != nil {
		return repository.File{}, err
	}
	mount, err := s.mounts.Get(source.Mount)
	if err != nil {
		return repository.File{}, err
	}
	stored, err := mount.Store.Copy(source.StorageKey, source.Name, source.ContentType)
	if err != nil {
		return repository.File{}, err
	}
	now := s.now().UTC()
	copy := source
	copy.ID = uuid.NewString()
	copy.FolderID = folderID
	copy.Mount = source.Mount
	copy.StorageKey = stored.Key
	copy.CreatedAt = now
	copy.UpdatedAt = now
	if err := s.repo.CreateFile(ctx, copy); err != nil {
		_ = mount.Store.Delete(stored.Key)
		return repository.File{}, err
	}
	return copy, nil
}

// PermanentlyDelete removes metadata and object data from its disk.
func (s *FileService) PermanentlyDelete(ctx context.Context, id string) error {
	item, err := s.repo.FindTrashedFile(ctx, id)
	if err != nil {
		return err
	}
	if err := s.repo.DeleteFilePermanently(ctx, id); err != nil {
		return err
	}
	mount, err := s.mounts.Get(item.Mount)
	if err != nil {
		return err
	}
	if err := mount.Store.Delete(item.StorageKey); err != nil {
		return err
	}
	_ = s.repo.Audit(ctx, repository.AuditEvent{ID: uuid.NewString(), Action: "purge", ResourceID: id, CreatedAt: s.now().UTC()})
	return nil
}
