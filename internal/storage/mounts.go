package storage

import (
	"errors"
	"fmt"
	"strings"
)

// ErrMountNotFound is returned when a storage mount id is unknown.
var ErrMountNotFound = errors.New("mount not found")

// MountSpec describes one named storage root before it is opened.
type MountSpec struct {
	ID   string
	Name string
	Root string
}

// Mount is a named storage root with its own object store. Each mount is an
// independent file system (a local disk, an attached drive, a network mount),
// so disk usage is reported per mount and files live on the mount they were
// uploaded to.
type Mount struct {
	ID    string
	Name  string
	Store *LocalStore
}

// Mounts manages the configured storage mounts in configuration order.
type Mounts struct {
	byID  map[string]*Mount
	order []*Mount
}

// NewMounts builds a Mounts manager, creating every storage root on disk.
// The first mount is the default, used when a request does not name one.
func NewMounts(specs []MountSpec, maxBytes int64, allowedMIMEs []string) (*Mounts, error) {
	if len(specs) == 0 {
		return nil, fmt.Errorf("at least one storage mount is required")
	}
	mounts := &Mounts{byID: make(map[string]*Mount, len(specs))}
	seen := make(map[string]struct{}, len(specs))
	for _, spec := range specs {
		if strings.TrimSpace(spec.ID) == "" || strings.TrimSpace(spec.Root) == "" {
			return nil, fmt.Errorf("storage mounts must have a name and a root path")
		}
		if _, ok := seen[spec.ID]; ok {
			return nil, fmt.Errorf("duplicate storage mount id %q", spec.ID)
		}
		seen[spec.ID] = struct{}{}
		store, err := NewLocalStore(spec.Root, maxBytes, allowedMIMEs)
		if err != nil {
			return nil, fmt.Errorf("mount %q: %w", spec.ID, err)
		}
		mounts.byID[spec.ID] = &Mount{ID: spec.ID, Name: spec.Name, Store: store}
		mounts.order = append(mounts.order, mounts.byID[spec.ID])
	}
	return mounts, nil
}

// Get returns the mount with the given id. An empty id resolves to the
// default (first configured) mount.
func (m *Mounts) Get(id string) (*Mount, error) {
	if id == "" {
		id = m.Default().ID
	}
	mount, ok := m.byID[id]
	if !ok {
		return nil, ErrMountNotFound
	}
	return mount, nil
}

// Default returns the first configured mount.
func (m *Mounts) Default() *Mount { return m.order[0] }

// List returns all mounts in configuration order.
func (m *Mounts) List() []*Mount { return m.order }
