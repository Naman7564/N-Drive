package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"fileservice/internal/config"
	"fileservice/internal/storage"
)

func TestUploadWriteBudget(t *testing.T) {
	tests := []struct {
		name          string
		contentLength int64
		wantAtLeast   time.Duration
	}{
		{"unknown length falls back to the minimum", 0, 30 * time.Minute},
		{"small file gets the minimum", 1 << 20, 30 * time.Minute},
		{"1 GiB file stays at the read-timeout floor", 1 << 30, 30 * time.Minute},
		{"2 GiB file scales with size", 2 << 30, 36 * time.Minute},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := uploadWriteBudget(tt.contentLength); got < tt.wantAtLeast {
				t.Fatalf("uploadWriteBudget(%d) = %s, want >= %s", tt.contentLength, got, tt.wantAtLeast)
			}
		})
	}
}

func TestDownloadWriteBudget(t *testing.T) {
	tests := []struct {
		name        string
		size        int64
		wantAtLeast time.Duration
	}{
		{"small file gets the minimum", 1 << 20, 30 * time.Minute},
		{"1 GiB file stays at the 30-minute floor", 1 << 30, 30 * time.Minute},
		{"8 GiB file scales with size", 8 << 30, 9 * time.Hour},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := downloadWriteBudget(tt.size); got < tt.wantAtLeast {
				t.Fatalf("downloadWriteBudget(%d) = %s, want >= %s", tt.size, got, tt.wantAtLeast)
			}
		})
	}
}

// slowWrite delays each response write to simulate a slow client connection,
// so a transfer outlives a write deadline without depending on kernel socket
// buffering. Unwrap is required so http.NewResponseController inside the
// handler can still reach the real connection.
type slowWrite struct {
	http.ResponseWriter
	delay time.Duration
}

func (s *slowWrite) Write(p []byte) (int, error) {
	time.Sleep(s.delay)
	return s.ResponseWriter.Write(p)
}

func (s *slowWrite) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// TestDownloadSlowReaderStillCompletes proves the download route delivers the
// whole stream even when the client is slow enough that the transfer outlives
// the server-wide write timeout. The handler extends the write deadline for
// downloads; without that, the stream is cut off mid-transfer.
func TestDownloadSlowReaderStillCompletes(t *testing.T) {
	cfg := testConfig(t)
	cfg.Storage.MaxBytes = 4 << 20 // the test uploads a 4 MiB object
	handler, closeDatabase := NewRouterWithCloser(context.Background(), slog.Default(), cfg)
	defer closeDatabase()

	// Simulate the server-wide write timeout exactly as http.Server does: a
	// deadline set on the connection before the handler runs, plus writes
	// delayed as they would be by a slow client. Applied to the download route
	// only, so the other requests in this test stay fast.
	const (
		downloadDeadline = 300 * time.Millisecond
		writeDelay       = 120 * time.Millisecond
	)
	ts := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/files/") && strings.HasSuffix(r.URL.Path, "/download") {
			_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(downloadDeadline))
			w = &slowWrite{ResponseWriter: w, delay: writeDelay}
		}
		handler.ServeHTTP(w, r)
	}))
	ts.Start()
	defer ts.Close()

	// Sign in and keep only the bearer token: cookie-free requests are not
	// subject to the CSRF check.
	loginBody := bytes.NewBufferString(`{"username":"Naman","password":"7564"}`)
	login, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/auth/login", loginBody)
	login.Header.Set("Content-Type", "application/json")
	loginResponse, err := http.DefaultClient.Do(login)
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(loginResponse.Body).Decode(&payload); err != nil {
		loginResponse.Body.Close()
		t.Fatalf("decode login: %v", err)
	}
	loginResponse.Body.Close()
	if payload.AccessToken == "" {
		t.Fatal("login did not return an access token")
	}

	// Upload a 4 MiB text file so we have a real stored object to download.
	// The download route streams in 1 MiB chunks, so the size guarantees
	// several chunk writes, each of which is guaranteed to cross the simulated
	// write deadline.
	const fileSize = 4 << 20
	var uploadBody bytes.Buffer
	writer := multipart.NewWriter(&uploadBody)
	_ = writer.WriteField("folder_id", "")
	part, _ := writer.CreateFormFile("file", "slow-download.txt")
	if _, err := part.Write(bytes.Repeat([]byte{'a'}, fileSize)); err != nil {
		t.Fatal(err)
	}
	_ = writer.Close()
	uploadRequest, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/files/upload", &uploadBody)
	uploadRequest.Header.Set("Content-Type", writer.FormDataContentType())
	uploadRequest.Header.Set("Authorization", "Bearer "+payload.AccessToken)
	uploadResponse, err := http.DefaultClient.Do(uploadRequest)
	if err != nil {
		t.Fatal(err)
	}
	var uploaded struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(uploadResponse.Body).Decode(&uploaded); err != nil {
		uploadResponse.Body.Close()
		t.Fatalf("decode upload: %v", err)
	}
	uploadResponse.Body.Close()
	if uploaded.ID == "" {
		t.Fatal("upload did not return a file id")
	}

	// Download. The server's writes are delayed 120 ms each, so the ~0.5 s
	// transfer outlives the 300 ms simulated write deadline and the stream
	// must survive several deadline crossings mid-transfer.
	downloadRequest, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/files/"+uploaded.ID+"/download", nil)
	downloadRequest.Header.Set("Authorization", "Bearer "+payload.AccessToken)
	started := time.Now()
	downloadResponse, err := http.DefaultClient.Do(downloadRequest)
	if err != nil {
		t.Fatalf("download request failed after %s: %v", time.Since(started), err)
	}
	defer downloadResponse.Body.Close()
	if downloadResponse.StatusCode != http.StatusOK {
		t.Fatalf("download status = %d after %s", downloadResponse.StatusCode, time.Since(started))
	}
	data, readErr := io.ReadAll(downloadResponse.Body)
	if readErr != nil {
		t.Fatalf("download cut off mid-stream after %s: %v", time.Since(started), readErr)
	}
	if len(data) != fileSize {
		t.Fatalf("received %d bytes, want %d", len(data), fileSize)
	}
	if elapsed := time.Since(started); elapsed < downloadDeadline {
		t.Fatalf("download finished in %s; expected it to outlive the %s write deadline for a real regression check", elapsed, downloadDeadline)
	}
}

// TestUploadSlowBodyStillGetsResponse proves the upload route delivers its
// completion response even when the request takes longer than the server-wide
// write timeout. The handler extends the write deadline for uploads; without
// that, the response write times out and the client never learns the file was
// saved, leaving it stuck at 100% until a page refresh.
func TestUploadSlowBodyStillGetsResponse(t *testing.T) {
	cfg := testConfig(t)
	handler, closeDatabase := NewRouterWithCloser(context.Background(), slog.Default(), cfg)
	defer closeDatabase()

	// Simulate the server-wide write timeout exactly as http.Server does: a
	// deadline set on the connection before the handler runs. Applied to the
	// upload route only, so the bcrypt-heavy login flow stays fast. The
	// upload handler must extend this deadline or the completion response is
	// cut off.
	const uploadDeadline = 500 * time.Millisecond
	ts := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/files/upload" {
			_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(uploadDeadline))
		}
		handler.ServeHTTP(w, r)
	}))
	ts.Start()
	defer ts.Close()

	// Sign in and keep only the bearer token: cookie-free requests are not
	// subject to the CSRF check.
	loginBody := bytes.NewBufferString(`{"username":"Naman","password":"7564"}`)
	login, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/auth/login", loginBody)
	login.Header.Set("Content-Type", "application/json")
	loginResponse, err := http.DefaultClient.Do(login)
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(loginResponse.Body).Decode(&payload); err != nil {
		loginResponse.Body.Close()
		t.Fatalf("decode login: %v", err)
	}
	loginResponse.Body.Close()
	if payload.AccessToken == "" {
		t.Fatal("login did not return an access token")
	}

	// Stream a multipart body slowly: 4 KiB every 500 ms, so the whole request
	// takes ~2 s, well past the 500 ms simulated write deadline.
	pr, pw := io.Pipe()
	writer := multipart.NewWriter(pw)
	go func() {
		defer pw.Close()
		_ = writer.WriteField("folder_id", "")
		part, err := writer.CreateFormFile("file", "slow.txt")
		if err != nil {
			return
		}
		const (
			chunk      = 4 << 10
			totalBytes = 16 << 10
		)
		buf := bytes.Repeat([]byte{'a'}, chunk)
		for written := 0; written < totalBytes; written += chunk {
			time.Sleep(500 * time.Millisecond)
			if _, err := part.Write(buf); err != nil {
				return
			}
		}
		_ = writer.Close()
	}()

	upload, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/files/upload", pr)
	upload.Header.Set("Content-Type", writer.FormDataContentType())
	upload.Header.Set("Authorization", "Bearer "+payload.AccessToken)

	started := time.Now()
	uploadResponse, err := http.DefaultClient.Do(upload)
	if err != nil {
		t.Fatalf("upload failed after %s: %v", time.Since(started), err)
	}
	defer uploadResponse.Body.Close()
	if uploadResponse.StatusCode != http.StatusCreated {
		data, _ := io.ReadAll(uploadResponse.Body)
		t.Fatalf("upload status = %d after %s, body = %s", uploadResponse.StatusCode, time.Since(started), data)
	}
	if elapsed := time.Since(started); elapsed < uploadDeadline {
		t.Fatalf("upload finished in %s; expected it to outlive the %s write deadline for a real regression check", elapsed, uploadDeadline)
	}
	var file struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(uploadResponse.Body).Decode(&file); err != nil {
		t.Fatalf("decode upload response: %v", err)
	}
	if file.Name != "slow.txt" {
		t.Fatalf("uploaded file name = %q, want slow.txt", file.Name)
	}
}

func TestSingleByteRange(t *testing.T) {
	tests := []struct {
		name       string
		value      string
		size       int64
		wantStart  int64
		wantLength int64
		wantMode   int
	}{
		{"no header", "", 100, 0, 0, rangeIgnore},
		{"non-byte unit", "items=0-10", 100, 0, 0, rangeIgnore},
		{"multi range is ignored", "bytes=0-9,20-29", 100, 0, 0, rangeIgnore},
		{"closed range", "bytes=10-19", 100, 10, 10, rangeServe},
		{"open ended", "bytes=90-", 100, 90, 10, rangeServe},
		{"end past EOF is clamped", "bytes=90-200", 100, 90, 10, rangeServe},
		{"suffix", "bytes=-10", 100, 90, 10, rangeServe},
		{"suffix longer than file", "bytes=-500", 100, 0, 100, rangeServe},
		{"start beyond EOF is unsatisfiable", "bytes=100-", 100, 0, 0, rangeUnsatisfiable},
		{"invalid syntax ignored", "bytes=abc-", 100, 0, 0, rangeIgnore},
		{"reversed range ignored", "bytes=20-10", 100, 0, 0, rangeIgnore},
		{"empty file ignores all ranges", "bytes=0-10", 0, 0, 0, rangeIgnore},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, length, mode := singleByteRange(tt.value, tt.size)
			if start != tt.wantStart || length != tt.wantLength || mode != tt.wantMode {
				t.Fatalf("singleByteRange(%q, %d) = (%d, %d, %d), want (%d, %d, %d)", tt.value, tt.size, start, length, mode, tt.wantStart, tt.wantLength, tt.wantMode)
			}
		})
	}
}

// TestDownloadRange verifies the download route answers single-range requests
// with 206 partial content and the right byte window, and 416 for ranges that
// start past the end of the file.
func TestDownloadRange(t *testing.T) {
	cfg := testConfig(t)
	handler, closeDatabase := NewRouterWithCloser(context.Background(), slog.Default(), cfg)
	defer closeDatabase()
	ts := httptest.NewServer(handler)
	defer ts.Close()

	login, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/auth/login", bytes.NewBufferString(`{"username":"Naman","password":"7564"}`))
	login.Header.Set("Content-Type", "application/json")
	loginResponse, err := http.DefaultClient.Do(login)
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(loginResponse.Body).Decode(&payload); err != nil {
		loginResponse.Body.Close()
		t.Fatalf("decode login: %v", err)
	}
	loginResponse.Body.Close()

	content := []byte("0123456789abcdefghij") // 20 bytes
	var uploadBody bytes.Buffer
	writer := multipart.NewWriter(&uploadBody)
	_ = writer.WriteField("folder_id", "")
	part, _ := writer.CreateFormFile("file", "range.txt")
	if _, err := part.Write(content); err != nil {
		t.Fatal(err)
	}
	_ = writer.Close()
	upload, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/files/upload", &uploadBody)
	upload.Header.Set("Content-Type", writer.FormDataContentType())
	upload.Header.Set("Authorization", "Bearer "+payload.AccessToken)
	uploadResponse, err := http.DefaultClient.Do(upload)
	if err != nil {
		t.Fatal(err)
	}
	var uploaded struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(uploadResponse.Body).Decode(&uploaded); err != nil {
		uploadResponse.Body.Close()
		t.Fatalf("decode upload: %v", err)
	}
	uploadResponse.Body.Close()
	if uploaded.ID == "" {
		t.Fatal("upload did not return a file id")
	}

	get := func(rangeHeader string) (*http.Response, []byte) {
		req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/files/"+uploaded.ID+"/download", nil)
		req.Header.Set("Authorization", "Bearer "+payload.AccessToken)
		if rangeHeader != "" {
			req.Header.Set("Range", rangeHeader)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("download request: %v", err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return resp, body
	}

	resp, body := get("bytes=5-9")
	if resp.StatusCode != http.StatusPartialContent {
		t.Fatalf("range status = %d, want 206", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Range"); got != "bytes 5-9/20" {
		t.Fatalf("Content-Range = %q", got)
	}
	if got := resp.Header.Get("Content-Length"); got != "5" {
		t.Fatalf("Content-Length = %q", got)
	}
	if string(body) != "56789" {
		t.Fatalf("range body = %q, want 56789", body)
	}

	resp, body = get("bytes=15-")
	if resp.StatusCode != http.StatusPartialContent || string(body) != "fghij" {
		t.Fatalf("open-ended range: status=%d body=%q", resp.StatusCode, body)
	}

	resp, body = get("bytes=-5")
	if resp.StatusCode != http.StatusPartialContent || string(body) != "fghij" {
		t.Fatalf("suffix range: status=%d body=%q", resp.StatusCode, body)
	}

	resp, _ = get("bytes=100-")
	if resp.StatusCode != http.StatusRequestedRangeNotSatisfiable {
		t.Fatalf("out-of-range status = %d, want 416", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Range"); got != "bytes */20" {
		t.Fatalf("416 Content-Range = %q", got)
	}

	resp, body = get("")
	if resp.StatusCode != http.StatusOK || string(body) != string(content) {
		t.Fatalf("full download: status=%d body=%q", resp.StatusCode, body)
	}
}

func TestDownloadHead(t *testing.T) {
	cfg := testConfig(t)
	handler, closeDatabase := NewRouterWithCloser(context.Background(), slog.Default(), cfg)
	defer closeDatabase()
	ts := httptest.NewServer(handler)
	defer ts.Close()

	login, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/auth/login", bytes.NewBufferString(`{"username":"Naman","password":"7564"}`))
	login.Header.Set("Content-Type", "application/json")
	loginResponse, err := http.DefaultClient.Do(login)
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(loginResponse.Body).Decode(&payload); err != nil {
		loginResponse.Body.Close()
		t.Fatalf("decode login: %v", err)
	}
	loginResponse.Body.Close()

	var uploadBody bytes.Buffer
	writer := multipart.NewWriter(&uploadBody)
	part, _ := writer.CreateFormFile("file", "head.txt")
	if _, err := part.Write(bytes.Repeat([]byte{'x'}, 1000)); err != nil {
		t.Fatal(err)
	}
	_ = writer.Close()
	upload, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/files/upload", &uploadBody)
	upload.Header.Set("Content-Type", writer.FormDataContentType())
	upload.Header.Set("Authorization", "Bearer "+payload.AccessToken)
	uploadResponse, err := http.DefaultClient.Do(upload)
	if err != nil {
		t.Fatal(err)
	}
	var uploaded struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(uploadResponse.Body).Decode(&uploaded); err != nil {
		uploadResponse.Body.Close()
		t.Fatalf("decode upload: %v", err)
	}
	uploadResponse.Body.Close()

	req, _ := http.NewRequest(http.MethodHead, ts.URL+"/api/files/"+uploaded.ID+"/download", nil)
	req.Header.Set("Authorization", "Bearer "+payload.AccessToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("HEAD status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Length"); got != "1000" {
		t.Fatalf("HEAD Content-Length = %q, want 1000", got)
	}
	body, _ := io.ReadAll(resp.Body)
	if len(body) != 0 {
		t.Fatalf("HEAD returned %d body bytes, want 0", len(body))
	}
}

// twoMountConfig returns a test config with two named disks: the default mount
// and a second "Media" mount.
func twoMountConfig(t *testing.T) config.Config {
	t.Helper()
	cfg := testConfig(t)
	cfg.Storage.Mounts = []storage.MountSpec{
		{ID: "default", Name: "Main", Root: t.TempDir()},
		{ID: "media", Name: "Media", Root: t.TempDir()},
	}
	return cfg
}

// TestMultiDiskFlow proves that folders and files live on the disk they were
// created on: listings are scoped per disk, the same root folder name can
// exist on different disks, files uploaded into a folder always land on that
// folder's disk, and the sidebar endpoints report each disk separately.
func TestMultiDiskFlow(t *testing.T) {
	handler, closeDatabase := NewRouterWithCloser(context.Background(), slog.Default(), twoMountConfig(t))
	defer closeDatabase()
	ts := httptest.NewServer(handler)
	defer ts.Close()

	login, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/auth/login", bytes.NewBufferString(`{"username":"Naman","password":"7564"}`))
	login.Header.Set("Content-Type", "application/json")
	loginResponse, err := http.DefaultClient.Do(login)
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(loginResponse.Body).Decode(&payload); err != nil {
		loginResponse.Body.Close()
		t.Fatalf("decode login: %v", err)
	}
	loginResponse.Body.Close()
	if payload.AccessToken == "" {
		t.Fatal("login did not return an access token")
	}
	bearer := func(r *http.Request) *http.Request {
		r.Header.Set("Authorization", "Bearer "+payload.AccessToken)
		return r
	}
	do := func(r *http.Request) *http.Response {
		t.Helper()
		resp, err := http.DefaultClient.Do(bearer(r))
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	postJSON := func(path string, body string) *http.Response {
		t.Helper()
		req, _ := http.NewRequest(http.MethodPost, ts.URL+path, bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		return do(req)
	}

	// The same root folder name is allowed on both disks.
	if resp := postJSON("/api/folders", `{"name":"Shared","mount":"default"}`); resp.StatusCode != http.StatusCreated {
		resp.Body.Close()
		t.Fatalf("create folder on default: status = %d", resp.StatusCode)
	} else {
		resp.Body.Close()
	}
	if resp := postJSON("/api/folders", `{"name":"Shared","mount":"media"}`); resp.StatusCode != http.StatusCreated {
		resp.Body.Close()
		t.Fatalf("create folder on media: status = %d", resp.StatusCode)
	} else {
		resp.Body.Close()
	}

	// Upload one file to each disk root. The expected mount is explicit so the
	// test documents that uploads into a folder land on the folder's disk even
	// when the request names a different disk.
	upload := func(mount, folderID, name, content, wantMount string) string {
		t.Helper()
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		if mount != "" {
			_ = writer.WriteField("mount", mount)
		}
		if folderID != "" {
			_ = writer.WriteField("folder_id", folderID)
		}
		part, _ := writer.CreateFormFile("file", name)
		if _, err := part.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
		_ = writer.Close()
		req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/files/upload", &body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		resp := do(req)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusCreated {
			data, _ := io.ReadAll(resp.Body)
			t.Fatalf("upload %s: status = %d, body = %s", name, resp.StatusCode, data)
		}
		var item struct {
			ID    string `json:"id"`
			Mount string `json:"mount"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&item); err != nil {
			t.Fatal(err)
		}
		if item.Mount != wantMount {
			t.Fatalf("upload %s: mount = %q, want %q", name, item.Mount, wantMount)
		}
		return item.ID
	}
	upload("default", "", "a.txt", "on main disk", "default")
	upload("media", "", "b.txt", "on media disk", "media")

	names := func(resp *http.Response) []string {
		t.Helper()
		defer resp.Body.Close()
		var payload struct {
			Items []struct {
				Name string `json:"name"`
			} `json:"items"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		var result []string
		for _, item := range payload.Items {
			result = append(result, item.Name)
		}
		return result
	}
	get := func(path string) *http.Response {
		t.Helper()
		req, _ := http.NewRequest(http.MethodGet, ts.URL+path, nil)
		return do(req)
	}

	folders := func(mount string) []string {
		return names(get("/api/folders?mount=" + mount))
	}
	files := func(mount string) []string {
		return names(get("/api/files?mount=" + mount))
	}
	if got := folders("default"); len(got) != 1 || got[0] != "Shared" {
		t.Fatalf("default disk folders = %v, want [Shared]", got)
	}
	if got := folders("media"); len(got) != 1 || got[0] != "Shared" {
		t.Fatalf("media disk folders = %v, want [Shared]", got)
	}
	if got := files("default"); len(got) != 1 || got[0] != "a.txt" {
		t.Fatalf("default disk files = %v, want [a.txt]", got)
	}
	if got := files("media"); len(got) != 1 || got[0] != "b.txt" {
		t.Fatalf("media disk files = %v, want [b.txt]", got)
	}

	// Uploading into a folder always lands on that folder's disk, even when
	// the request names a different disk.
	var shared struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	if err := json.NewDecoder(get("/api/folders?mount=default").Body).Decode(&shared); err != nil {
		t.Fatal(err)
	}
	defaultSharedID := shared.Items[0].ID
	upload("media", defaultSharedID, "c.txt", "inside a default-disk folder", "default")

	// The new file appears inside the default disk's folder listing...
	if got := names(get("/api/files?mount=default&folder_id=" + defaultSharedID)); len(got) != 1 || got[0] != "c.txt" {
		t.Fatalf("default-disk folder contents = %v, want [c.txt]", got)
	}
	// ...and the media disk root is untouched.
	if got := files("media"); len(got) != 1 || got[0] != "b.txt" {
		t.Fatalf("media disk files after folder upload = %v, want [b.txt]", got)
	}

	// The sidebar data: dashboard and disks both report both disks.
	for _, path := range []string{"/api/dashboard", "/api/disks"} {
		resp := get(path)
		var payload struct {
			Disks []struct {
				ID    string `json:"id"`
				Name  string `json:"name"`
				Total int64  `json:"total"`
				Files int64  `json:"files"`
			} `json:"disks"`
			Items []struct {
				ID    string `json:"id"`
				Name  string `json:"name"`
				Total int64  `json:"total"`
				Files int64  `json:"files"`
			} `json:"items"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			resp.Body.Close()
			t.Fatal(err)
		}
		resp.Body.Close()
		var disks []struct {
			ID    string
			Name  string
			Total int64
			Files int64
		}
		for _, disk := range payload.Disks {
			disks = append(disks, struct {
				ID    string
				Name  string
				Total int64
				Files int64
			}{disk.ID, disk.Name, disk.Total, disk.Files})
		}
		for _, disk := range payload.Items {
			disks = append(disks, struct {
				ID    string
				Name  string
				Total int64
				Files int64
			}{disk.ID, disk.Name, disk.Total, disk.Files})
		}
		if len(disks) != 2 {
			t.Fatalf("%s disks = %+v, want 2 disks", path, disks)
		}
		seen := map[string]bool{}
		for _, disk := range disks {
			seen[disk.ID] = true
			if disk.Total <= 0 {
				t.Fatalf("%s disk %q has zero capacity", path, disk.ID)
			}
		}
		if !seen["default"] || !seen["media"] {
			t.Fatalf("%s disks = %+v, want default and media", path, disks)
		}
	}
}
