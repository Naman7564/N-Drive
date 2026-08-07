package storage

import (
	"bytes"
	"errors"
	"fmt"
	"testing"

	"github.com/cespare/xxhash/v2"
)

func TestLocalStoreDiskUsage(t *testing.T) {
	store, err := NewLocalStore(t.TempDir(), 1024, nil)
	if err != nil {
		t.Fatal(err)
	}
	total, free, used := store.DiskUsage()
	if total <= 0 {
		t.Fatalf("total = %d, want > 0", total)
	}
	if free < 0 || used < 0 {
		t.Fatalf("free=%d used=%d, want non-negative", free, used)
	}
	if used > total || free > total {
		t.Fatalf("used=%d free=%d exceed total=%d", used, free, total)
	}
}

func TestLocalStoreSecurity(t *testing.T) {
	store, err := NewLocalStore(t.TempDir(), 1024, []string{"text/plain"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Save(bytes.NewReader(bytes.Repeat([]byte{'x'}, 1025)), "../notes.txt", "text/plain"); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("large file error = %v", err)
	}
	item, err := store.Save(bytes.NewReader([]byte("hello")), "../notes.txt", "text/plain")
	if err != nil {
		t.Fatal(err)
	}
	if item.Filename != "notes.txt" {
		t.Fatalf("filename = %q", item.Filename)
	}
	want := xxhash.Sum64String("hello")
	if item.Checksum != fmt.Sprintf("%x", want) {
		t.Fatalf("checksum = %q", item.Checksum)
	}
	if _, err := store.Open("../../outside"); !errors.Is(err, ErrTraversal) {
		t.Fatalf("traversal error = %v", err)
	}
	pngHeader := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}
	if _, err := store.Save(bytes.NewReader(pngHeader), "photo.png", "image/png"); !errors.Is(err, ErrInvalidMIME) {
		t.Fatalf("mime error = %v", err)
	}
}

func TestLocalStoreAcceptsBroadFileTypesByDefault(t *testing.T) {
	store, err := NewLocalStore(t.TempDir(), 5<<30, nil)
	if err != nil {
		t.Fatal(err)
	}
	item, err := store.Save(bytes.NewReader([]byte("<!doctype html><script>alert(1)</script>")), "page.html", "text/html")
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if item.ContentType != "text/html" {
		t.Fatalf("ContentType = %q, want text/html", item.ContentType)
	}
}

func TestLocalStoreFiveGiBBoundaryUsesStreamingLimit(t *testing.T) {
	store, err := NewLocalStore(t.TempDir(), 5, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Save(bytes.NewReader(bytes.Repeat([]byte{'x'}, 6)), "too-large.bin", "application/octet-stream"); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("large file error = %v", err)
	}
}
