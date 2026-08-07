package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
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
	// The server-wide write timeout starts when the request header is read,
	// before the body has arrived, so a slow or large upload can exceed it
	// before the completion response is written. That silently kills the
	// response and leaves the client stuck at 100% with no confirmation even
	// though the file was saved. Extend the write deadline for this request.
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(uploadWriteBudget(r.ContentLength)))
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
	// Same server-wide write timeout issue as uploads: the deadline starts
	// when the request header is read, so a large or slow download can be cut
	// off mid-stream. Extend the write deadline for this request.
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(downloadWriteBudget(item.Size)))
	file, err := h.store.Open(item.StorageKey)
	if err != nil {
		writeError(w, 404, "file content not found")
		return
	}
	defer file.Close()
	w.Header().Set("Content-Type", item.ContentType)
	w.Header().Set("Content-Disposition", "attachment; filename="+strconv.Quote(item.Name))
	w.Header().Set("Accept-Ranges", "bytes")
	_ = h.repo.Audit(r.Context(), repository.AuditEvent{ID: uuid.NewString(), Action: "download", ResourceID: item.ID, CreatedAt: now()})
	serveRange(w, r, file, item.Size)
}

// downloadChunkSize is the I/O chunk used when streaming downloads. A large
// buffer reduces syscalls and keeps throughput near the disk's raw read speed;
// http.ServeContent copies with the default 32 KiB buffer.
const downloadChunkSize = 1 << 20

const (
	rangeIgnore        = iota // serve the whole file
	rangeServe                // serve a single satisfiable byte range
	rangeUnsatisfiable        // valid range outside the file: answer 416
)

// singleByteRange parses an RFC 7233 single-range request. Malformed or
// multi-range headers are ignored (the caller serves the whole file), while a
// well-formed range that starts beyond the end of the file is reported as
// unsatisfiable so the caller can answer 416.
func singleByteRange(value string, size int64) (start, length int64, mode int) {
	if value == "" || size <= 0 || !strings.HasPrefix(value, "bytes=") {
		return 0, 0, rangeIgnore
	}
	spec := strings.TrimSpace(strings.TrimPrefix(value, "bytes="))
	if spec == "" || strings.Contains(spec, ",") {
		return 0, 0, rangeIgnore
	}
	dash := strings.IndexByte(spec, '-')
	if dash < 0 {
		return 0, 0, rangeIgnore
	}
	startText := strings.TrimSpace(spec[:dash])
	endText := strings.TrimSpace(spec[dash+1:])
	if startText == "" {
		// Suffix range: the last n bytes.
		n, err := strconv.ParseInt(endText, 10, 64)
		if err != nil || n <= 0 {
			return 0, 0, rangeIgnore
		}
		if n > size {
			n = size
		}
		return size - n, n, rangeServe
	}
	start, err := strconv.ParseInt(startText, 10, 64)
	if err != nil || start < 0 {
		return 0, 0, rangeIgnore
	}
	if start >= size {
		return 0, 0, rangeUnsatisfiable
	}
	end := size - 1
	if endText != "" {
		end, err = strconv.ParseInt(endText, 10, 64)
		if err != nil || end < start {
			return 0, 0, rangeIgnore
		}
		if end >= size {
			end = size - 1
		}
	}
	return start, end - start + 1, rangeServe
}

// serveRange writes the stored object, honoring a single HTTP Range request
// and streaming the body in large chunks. HEAD requests get headers only.
func serveRange(w http.ResponseWriter, r *http.Request, file io.ReadSeeker, size int64) {
	start, length, mode := singleByteRange(r.Header.Get("Range"), size)
	switch mode {
	case rangeUnsatisfiable:
		w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", size))
		w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
		return
	case rangeServe:
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, start+length-1, size))
		w.Header().Set("Content-Length", strconv.FormatInt(length, 10))
		w.WriteHeader(http.StatusPartialContent)
		if r.Method == http.MethodHead {
			return
		}
		if _, err := file.Seek(start, io.SeekStart); err != nil {
			return
		}
		_, _ = io.CopyBuffer(w, io.LimitReader(file, length), make([]byte, downloadChunkSize))
		return
	default:
		w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
		w.WriteHeader(http.StatusOK)
		if r.Method == http.MethodHead {
			return
		}
		_, _ = io.CopyBuffer(w, file, make([]byte, downloadChunkSize))
	}
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

// writeBudgetForBody returns a write deadline generous enough that receiving
// or serving a body of contentLength bytes is never cut off by the server-wide
// write timeout, which is measured from when the request header is read. It
// floors at minBudget and scales with the body size assuming at least
// minThroughput bytes per second of effective transfer, plus a small headroom
// for processing.
func writeBudgetForBody(contentLength, minThroughput int64, minBudget time.Duration) time.Duration {
	const headroom = 2 * time.Minute
	budget := minBudget
	if contentLength > 0 {
		if scaled := time.Duration(contentLength/minThroughput)*time.Second + headroom; scaled > budget {
			budget = scaled
		}
	}
	return budget
}

// uploadWriteBudget is how long the upload route may take before its response
// write is cut off. The floor matches the default read timeout so the write
// deadline can never be the constraint that kills the completion response for
// an upload that could otherwise complete, even over a slow connection.
func uploadWriteBudget(contentLength int64) time.Duration {
	return writeBudgetForBody(contentLength, 1<<20, 30*time.Minute)
}

// downloadWriteBudget is how long the download route may take before its
// stream is cut off. Downloads stream continuously, so the budget scales with
// the file size at a conservative 256 KiB/s floor; the 30-minute minimum
// covers every download the default 30s write timeout would have killed.
func downloadWriteBudget(size int64) time.Duration {
	return writeBudgetForBody(size, 1<<18, 30*time.Minute)
}
