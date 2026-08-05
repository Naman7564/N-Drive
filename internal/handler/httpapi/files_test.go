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

	// Upload a 512 KiB text file so we have a real stored object to download.
	// The size guarantees many chunk writes (io.Copy uses a 32 KiB buffer), so
	// several of them are guaranteed to cross the simulated write deadline.
	const fileSize = 512 << 10
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

	// Download. The server's writes are delayed 120 ms each, so the ~1 s
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
