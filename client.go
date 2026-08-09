package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"monarch-cli/pkg/monarch"
)

// defaultSessionFile is the legacy/default JSON session path
// (~/.config/monarch/session.json).
func defaultSessionFile() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "monarch", "session.json")
}

// sessionStore resolves where the session lives:
//  1. MONARCH_SESSION_FILE — explicit file override (any platform)
//  2. macOS — the login Keychain (encrypted at rest)
//  3. elsewhere — the default 0600 JSON file
func sessionStore() (monarch.SessionStore, error) {
	if p := os.Getenv("MONARCH_SESSION_FILE"); p != "" {
		return monarch.NewFileStore(p), nil
	}
	if runtime.GOOS == "darwin" {
		store, err := monarch.NewKeychainStore()
		if err != nil {
			return nil, err
		}
		migrateLegacySession(store, defaultSessionFile())
		return store, nil
	}
	return monarch.NewFileStore(defaultSessionFile()), nil
}

// migrateLegacySession moves a pre-Keychain session.json into the Keychain
// once, then removes the file. Best-effort: on any failure the file is left
// alone and a fresh login still works. Unknown legacy fields (like the old
// fabricated expiresAt) are dropped by JSON decoding.
func migrateLegacySession(store monarch.SessionStore, legacyPath string) {
	if _, err := store.Load(); !errors.Is(err, monarch.ErrNoSession) {
		return
	}
	legacy := monarch.NewFileStore(legacyPath)
	s, err := legacy.Load()
	if err != nil {
		return
	}
	if err := store.Save(s); err != nil {
		return
	}
	if _, err := store.Load(); err != nil {
		return // only remove the file once the Keychain round-trips
	}
	_ = legacy.Delete()
	fmt.Fprintf(os.Stderr, "note: migrated session from %s to the macOS Keychain\n", legacyPath)
}

// newClient builds an authenticated client. Auth resolution order:
//  1. MONARCH_TOKEN env var (direct bearer token)
//  2. saved session (Keychain on macOS, session file elsewhere)
func newClient(extra ...monarch.Option) (*monarch.Client, error) {
	if tok := os.Getenv("MONARCH_TOKEN"); tok != "" {
		return monarch.New(append([]monarch.Option{monarch.WithToken(tok)}, extra...)...)
	}
	store, err := sessionStore()
	if err != nil {
		return nil, err
	}
	c, err := monarch.New(append([]monarch.Option{monarch.WithSessionStore(store)}, extra...)...)
	if err != nil {
		return nil, err
	}
	if c.Session() == nil {
		return nil, errors.New("not logged in: run `monarch login` first (or set MONARCH_TOKEN)")
	}
	return c, nil
}

// newClientForLogin builds an unauthenticated client that persists its
// session on successful login.
func newClientForLogin() (*monarch.Client, error) {
	store, err := sessionStore()
	if err != nil {
		return nil, err
	}
	return monarch.New(monarch.WithSessionStore(store))
}
