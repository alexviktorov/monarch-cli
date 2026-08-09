package monarch

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Session is what login produces. There is deliberately no ExpiresAt: the
// server never reports a token lifetime, and fabricating one client-side
// (as upstream libraries do) causes false expiry refusals. A real 401 is
// the source of truth.
type Session struct {
	Token      string    `json:"token"`
	Email      string    `json:"email,omitempty"`
	UserID     string    `json:"userId,omitempty"`
	DeviceUUID string    `json:"deviceUuid"`
	CreatedAt  time.Time `json:"createdAt,omitzero"`
}

// SessionStore persists a session at rest.
type SessionStore interface {
	Load() (*Session, error) // ErrNoSession when absent
	Save(*Session) error
	Delete() error // idempotent
}

// fileStore keeps the session as a 0600 JSON file under a 0700 directory.
// It refuses symlinked paths and group/world-accessible parent directories:
// the path is env-overridable (MONARCH_SESSION_FILE), so it must not be
// usable as a write-through-symlink primitive.
type fileStore struct{ path string }

func NewFileStore(path string) SessionStore { return &fileStore{path: path} }

func (f *fileStore) Load() (*Session, error) {
	if err := refuseSymlink(f.path); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(f.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNoSession
	}
	if err != nil {
		return nil, err
	}
	var s Session
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("monarch: corrupt session file %s: %w", f.path, err)
	}
	if s.Token == "" {
		return nil, ErrNoSession
	}
	return &s, nil
}

func (f *fileStore) Save(s *Session) error {
	if err := refuseSymlink(f.path); err != nil {
		return err
	}
	dir := filepath.Dir(f.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	info, err := os.Stat(dir)
	if err != nil {
		return err
	}
	// A group/world-writable parent lets another local user swap the file
	// for a symlink between our checks and the rename. Read/traverse bits
	// are fine — the session file itself is 0600.
	if info.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("monarch: refusing to write session: %s is group/world writable (%04o)", dir, info.Mode().Perm())
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	// Write-to-temp + rename: atomic, and the secret never exists on disk
	// with permissions other than 0600 (CreateTemp creates 0600).
	tmp, err := os.CreateTemp(dir, ".session-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), f.path)
}

func (f *fileStore) Delete() error {
	err := os.Remove(f.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func refuseSymlink(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("monarch: refusing session path %s: it is a symlink", path)
	}
	return nil
}
