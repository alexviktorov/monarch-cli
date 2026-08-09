package monarch

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
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
	// TokenExpiration is informational: normally empty (trusted-device
	// tokens don't expire); set when the server reported a real expiry,
	// which usually means trusted_device wasn't honored.
	TokenExpiration string `json:"tokenExpiration,omitempty"`
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
	// os.Root pins the parent directory (immune to it being swapped for a
	// symlink mid-operation); O_NOFOLLOW refuses a symlinked file itself.
	root, err := os.OpenRoot(filepath.Dir(f.path))
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNoSession
	}
	if err != nil {
		return nil, err
	}
	defer root.Close()
	fh, err := root.OpenFile(filepath.Base(f.path), os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNoSession
	}
	if err != nil {
		return nil, fmt.Errorf("monarch: open session file %s: %w", f.path, err)
	}
	defer fh.Close()
	data, err := io.ReadAll(io.LimitReader(fh, 1<<20))
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
	// Also refuse a symlinked immediate parent (e.g. ~/.config/monarch
	// pointing into an attacker-owned directory).
	if err := refuseSymlink(dir); err != nil {
		return err
	}
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
	// os.Root pins the directory so every operation below resolves against
	// the fd, not the path — a concurrent swap of an ancestor for a symlink
	// cannot redirect the write. O_EXCL refuses a pre-planted temp entry
	// (including a symlink); write-to-temp + rename keeps the update atomic
	// and the secret never exists on disk with permissions other than 0600.
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	name := filepath.Base(f.path)
	tmp := name + ".tmp"
	_ = root.Remove(tmp)
	fh, err := root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := fh.Write(data); err != nil {
		fh.Close()
		_ = root.Remove(tmp)
		return err
	}
	if err := fh.Close(); err != nil {
		_ = root.Remove(tmp)
		return err
	}
	if err := root.Rename(tmp, name); err != nil {
		_ = root.Remove(tmp)
		return err
	}
	return nil
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
