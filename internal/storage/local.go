package storage

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

var (
	ErrInvalidFilename = errors.New("invalid filename")
	ErrInvalidMIME     = errors.New("invalid mime type")
	ErrTooLarge        = errors.New("file too large")
	ErrTraversal       = errors.New("invalid storage path")
)

var safeFilename = regexp.MustCompile(`[^A-Za-z0-9._ -]+`)

// FileInfo describes a stored object.
type FileInfo struct {
	Key         string
	Filename    string
	Size        int64
	ContentType string
	Checksum    string
}

// LocalStore stores objects below one configured root directory.
type LocalStore struct {
	root         string
	maxBytes     int64
	allowedMIMEs map[string]struct{}
}

// NewLocalStore creates a filesystem-backed store.
func NewLocalStore(root string, maxBytes int64, allowedMIMEs []string) (*LocalStore, error) {
	if strings.TrimSpace(root) == "" || maxBytes <= 0 {
		return nil, fmt.Errorf("storage root and max size are required")
	}
	if err := os.MkdirAll(root, 0o750); err != nil {
		return nil, fmt.Errorf("create storage root: %w", err)
	}
	allowed := make(map[string]struct{}, len(allowedMIMEs))
	for _, value := range allowedMIMEs {
		value = strings.ToLower(strings.TrimSpace(value))
		if value != "" {
			allowed[value] = struct{}{}
		}
	}
	return &LocalStore{root: root, maxBytes: maxBytes, allowedMIMEs: allowed}, nil
}

// SanitizeFilename returns a safe display name without path components.
func SanitizeFilename(filename string) (string, error) {
	filename = strings.TrimSpace(filepath.Base(filename))
	filename = safeFilename.ReplaceAllString(filename, "")
	filename = strings.TrimSpace(filename)
	if filename == "" || filename == "." || filename == ".." || len(filename) > 255 {
		return "", ErrInvalidFilename
	}
	return filename, nil
}

// Save streams a file into a generated object key and computes its checksum.
func (s *LocalStore) Save(src io.Reader, filename, _ string) (FileInfo, error) {
	cleanName, err := SanitizeFilename(filename)
	if err != nil {
		return FileInfo{}, err
	}
	objectID := uuid.NewString()
	key := filepath.ToSlash(filepath.Join(objectID[:2], objectID))
	path, err := s.safePath(key)
	if err != nil {
		return FileInfo{}, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return FileInfo{}, fmt.Errorf("create object directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o640)
	if err != nil {
		return FileInfo{}, fmt.Errorf("create object: %w", err)
	}
	cleanup := func() { _ = file.Close(); _ = os.Remove(path) }

	hasher := sha256.New()
	writer := io.MultiWriter(file, hasher)
	written, err := io.Copy(writer, io.LimitReader(src, s.maxBytes+1))
	if err != nil {
		cleanup()
		return FileInfo{}, fmt.Errorf("write object: %w", err)
	}
	if written > s.maxBytes {
		cleanup()
		return FileInfo{}, ErrTooLarge
	}
	contentType := normalizeMIME(detectFileMIME(path))
	if len(s.allowedMIMEs) > 0 {
		if _, ok := s.allowedMIMEs[contentType]; !ok {
			cleanup()
			return FileInfo{}, ErrInvalidMIME
		}
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return FileInfo{}, fmt.Errorf("close object: %w", err)
	}
	return FileInfo{Key: key, Filename: cleanName, Size: written, ContentType: contentType, Checksum: fmt.Sprintf("%x", hasher.Sum(nil))}, nil
}

// Copy duplicates an object under a new generated key.
func (s *LocalStore) Copy(key, filename, contentType string) (FileInfo, error) {
	file, err := s.Open(key)
	if err != nil {
		return FileInfo{}, err
	}
	defer file.Close()
	return s.Save(file, filename, contentType)
}

// Open returns a stored object after validating its generated key.
func (s *LocalStore) Open(key string) (*os.File, error) {
	path, err := s.safePath(key)
	if err != nil {
		return nil, err
	}
	return os.Open(path)
}

// Delete removes a stored object.
func (s *LocalStore) Delete(key string) error {
	path, err := s.safePath(key)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("delete object: %w", err)
	}
	return nil
}

// DiskUsage reports the total, free, and used bytes on the filesystem that
// contains the storage root (server disk availability).
func (s *LocalStore) DiskUsage() (total, free, used int64) {
	return diskUsage(s.root)
}

func (s *LocalStore) safePath(key string) (string, error) {
	if key == "" || filepath.IsAbs(key) || strings.ContainsAny(key, `\\`) {
		return "", ErrTraversal
	}
	clean := filepath.Clean(filepath.FromSlash(key))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", ErrTraversal
	}
	root, err := filepath.Abs(s.root)
	if err != nil {
		return "", err
	}
	path, err := filepath.Abs(filepath.Join(root, clean))
	if err != nil || (path != root && !strings.HasPrefix(path, root+string(filepath.Separator))) {
		return "", ErrTraversal
	}
	return path, nil
}

func normalizeMIME(value string) string {
	mediaType, _, err := mime.ParseMediaType(strings.ToLower(strings.TrimSpace(value)))
	if err != nil || mediaType == "" {
		return strings.ToLower(strings.TrimSpace(value))
	}
	return mediaType
}

func detectFileMIME(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return "application/octet-stream"
	}
	defer file.Close()
	buffer := make([]byte, 512)
	n, _ := file.Read(buffer)
	value := http.DetectContentType(buffer[:n])
	if extension := mime.TypeByExtension(filepath.Ext(path)); value == "application/octet-stream" && extension != "" {
		return extension
	}
	return value
}
