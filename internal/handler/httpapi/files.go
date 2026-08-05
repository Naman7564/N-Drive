package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"fileservice/internal/repository"
	"fileservice/internal/service"
	"fileservice/internal/storage"
)

type fileHandler struct {
	service         *service.FileService
	repo            *repository.FileRepository
	store           *storage.LocalStore
	serviceMaxBytes int64
}

func page(r *http.Request) (int, int) {
	limit := 50
	offset := 0
	if value, _ := strconv.Atoi(r.URL.Query().Get("limit")); value > 0 {
		limit = value
	}
	if limit > 100 {
		limit = 100
	}
	if value, _ := strconv.Atoi(r.URL.Query().Get("offset")); value >= 0 {
		offset = value
	}
	return limit, offset
}
func encodeData(w http.ResponseWriter, value any) { writeJSON(w, http.StatusOK, value) }

func (h *fileHandler) listFolders(w http.ResponseWriter, r *http.Request) {
	limit, offset := page(r)
	items, err := h.repo.ListFolders(r.Context(), r.URL.Query().Get("parent_id"), limit, offset)
	if err != nil {
		writeError(w, 500, "could not list folders")
		return
	}
	encodeData(w, map[string]any{"items": items, "limit": limit, "offset": offset})
}
func (h *fileHandler) createFolder(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ParentID string `json:"parent_id"`
		Name     string `json:"name"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	item, err := h.service.CreateFolder(r.Context(), body.ParentID, strings.TrimSpace(body.Name))
	if err != nil {
		if errors.Is(err, repository.ErrConflict) {
			writeError(w, 409, "folder already exists")
		} else {
			writeError(w, 400, err.Error())
		}
		return
	}
	writeJSON(w, 201, item)
}
func (h *fileHandler) renameFolder(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		Name string `json:"name"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if err := h.service.RenameFolder(r.Context(), id, strings.TrimSpace(body.Name)); err != nil {
		if errors.Is(err, repository.ErrConflict) {
			writeError(w, 409, "folder already exists")
		} else if strings.Contains(err.Error(), "folder name") {
			writeError(w, 400, err.Error())
		} else {
			writeError(w, 404, "folder not found")
		}
		return
	}
	w.WriteHeader(204)
}
func (h *fileHandler) deleteFolder(w http.ResponseWriter, r *http.Request) {
	if err := h.repo.DeleteFolder(r.Context(), r.PathValue("id")); err != nil {
		if errors.Is(err, repository.ErrConflict) {
			writeError(w, 409, "folder is not empty")
		} else {
			writeError(w, 404, "folder not found")
		}
		return
	}
	w.WriteHeader(204)
}

func (h *fileHandler) upload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, h.storeMaxBytes()+1<<20)
	reader, err := r.MultipartReader()
	if err != nil {
		writeError(w, 400, "multipart upload required")
		return
	}
	var folderID string
	var result repository.File
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			writeError(w, 400, "invalid multipart upload")
			return
		}
		if part.FormName() == "folder_id" {
			data, _ := io.ReadAll(io.LimitReader(part, 1024))
			folderID = strings.TrimSpace(string(data))
			continue
		}
		if part.FormName() != "file" {
			continue
		}
		result, err = h.service.Upload(r.Context(), folderID, part.FileName(), part.Header.Get("Content-Type"), part)
		if err != nil {
			var maxBytesErr *http.MaxBytesError
			if errors.Is(err, storage.ErrTooLarge) || errors.As(err, &maxBytesErr) {
				writeError(w, 413, "file too large")
			} else if errors.Is(err, storage.ErrInvalidMIME) {
				writeError(w, 415, "file type is not allowed")
			} else {
				writeError(w, 400, "upload failed")
			}
			return
		}
		break
	}
	if result.ID == "" {
		writeError(w, 400, "file part is required")
		return
	}
	writeJSON(w, 201, result)
}
func (h *fileHandler) listFiles(w http.ResponseWriter, r *http.Request) {
	limit, offset := page(r)
	items, err := h.repo.ListFiles(r.Context(), r.URL.Query().Get("folder_id"), limit, offset)
	if err != nil {
		writeError(w, 500, "could not list files")
		return
	}
	encodeData(w, map[string]any{"items": items, "limit": limit, "offset": offset})
}
func (h *fileHandler) download(w http.ResponseWriter, r *http.Request) {
	item, err := h.repo.FindFile(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, 404, "file not found")
		return
	}
	file, err := h.store.Open(item.StorageKey)
	if err != nil {
		writeError(w, 404, "file content not found")
		return
	}
	defer file.Close()
	w.Header().Set("Content-Type", item.ContentType)
	w.Header().Set("Content-Disposition", "attachment; filename="+strconv.Quote(item.Name))
	_ = h.repo.Audit(r.Context(), repository.AuditEvent{ID: uuid.NewString(), Action: "download", ResourceID: item.ID, CreatedAt: now()})
	http.ServeContent(w, r, item.Name, item.UpdatedAt, file)
}
func (h *fileHandler) copyFile(w http.ResponseWriter, r *http.Request) {
	var body struct {
		FolderID string `json:"folder_id"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body)
	}
	item, err := h.service.CopyFile(r.Context(), r.PathValue("id"), body.FolderID)
	if err != nil {
		writeError(w, 404, "file not found")
		return
	}
	writeJSON(w, 201, item)
}
func (h *fileHandler) renameFile(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if err := h.service.RenameFile(r.Context(), r.PathValue("id"), strings.TrimSpace(body.Name)); err != nil {
		writeError(w, 404, "file not found")
		return
	}
	w.WriteHeader(204)
}
func (h *fileHandler) moveFile(w http.ResponseWriter, r *http.Request) {
	var body struct {
		FolderID string `json:"folder_id"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if err := h.service.MoveFile(r.Context(), r.PathValue("id"), body.FolderID); err != nil {
		writeError(w, 404, "file or folder not found")
		return
	}
	w.WriteHeader(204)
}
func (h *fileHandler) deleteFile(w http.ResponseWriter, r *http.Request) {
	if err := h.service.DeleteFile(r.Context(), r.PathValue("id")); err != nil {
		writeError(w, 404, "file not found")
		return
	}
	w.WriteHeader(204)
}
func (h *fileHandler) trash(w http.ResponseWriter, r *http.Request) {
	limit, offset := page(r)
	items, err := h.repo.ListTrash(r.Context(), limit, offset)
	if err != nil {
		writeError(w, 500, "could not list trash")
		return
	}
	encodeData(w, map[string]any{"items": items, "limit": limit, "offset": offset})
}
func (h *fileHandler) restore(w http.ResponseWriter, r *http.Request) {
	if err := h.service.RestoreFile(r.Context(), r.PathValue("id")); err != nil {
		writeError(w, 404, "trash item not found")
		return
	}
	w.WriteHeader(204)
}
func (h *fileHandler) purge(w http.ResponseWriter, r *http.Request) {
	if err := h.service.PermanentlyDelete(r.Context(), r.PathValue("id")); err != nil {
		writeError(w, 404, "trash item not found")
		return
	}
	w.WriteHeader(204)
}
func (h *fileHandler) search(w http.ResponseWriter, r *http.Request) {
	limit, offset := page(r)
	items, err := h.repo.Search(r.Context(), r.URL.Query().Get("q"), limit, offset)
	if err != nil {
		writeError(w, 500, "search failed")
		return
	}
	encodeData(w, map[string]any{"items": items, "limit": limit, "offset": offset})
}
func (h *fileHandler) dashboard(w http.ResponseWriter, r *http.Request) {
	data, err := h.repo.Dashboard(r.Context())
	if err != nil {
		writeError(w, 500, "dashboard unavailable")
		return
	}
	total, free, used := h.store.DiskUsage()
	encodeData(w, map[string]any{
		"files": data["files"], "folders": data["folders"], "bytes": data["bytes"], "trash": data["trash"],
		"storage": map[string]int64{"total": total, "free": free, "used": used},
	})
}
func now() time.Time                        { return time.Now().UTC() }
func (h *fileHandler) storeMaxBytes() int64 { return h.serviceMaxBytes }
