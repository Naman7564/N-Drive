package storage

import (
	"errors"
	"testing"
)

func TestMountsBasics(t *testing.T) {
	mounts, err := NewMounts([]MountSpec{
		{ID: "default", Name: "Main", Root: t.TempDir()},
		{ID: "media", Name: "Media", Root: t.TempDir()},
	}, 1<<20, nil)
	if err != nil {
		t.Fatal(err)
	}

	if got := mounts.Default().ID; got != "default" {
		t.Fatalf("Default().ID = %q, want default", got)
	}
	mount, err := mounts.Get("media")
	if err != nil {
		t.Fatal(err)
	}
	if mount.Name != "Media" {
		t.Fatalf("Get(media).Name = %q, want Media", mount.Name)
	}
	if got, err := mounts.Get(""); err != nil || got.ID != "default" {
		t.Fatalf("Get(\"\") = (%q, %v), want default mount", got.ID, err)
	}
	if _, err := mounts.Get("missing"); !errors.Is(err, ErrMountNotFound) {
		t.Fatalf("Get(missing) error = %v, want ErrMountNotFound", err)
	}
	list := mounts.List()
	if len(list) != 2 || list[0].ID != "default" || list[1].ID != "media" {
		t.Fatalf("List() order = %v, want configuration order", list)
	}
}

func TestNewMountsRejectsInvalidSpecs(t *testing.T) {
	if _, err := NewMounts(nil, 1<<20, nil); err == nil {
		t.Fatal("NewMounts() error = nil, want error for no mounts")
	}
	if _, err := NewMounts([]MountSpec{{ID: "a", Name: "A", Root: ""}}, 1<<20, nil); err == nil {
		t.Fatal("NewMounts() error = nil, want error for empty root")
	}
	if _, err := NewMounts([]MountSpec{
		{ID: "same", Name: "A", Root: t.TempDir()},
		{ID: "same", Name: "B", Root: t.TempDir()},
	}, 1<<20, nil); err == nil {
		t.Fatal("NewMounts() error = nil, want error for duplicate ids")
	}
}
