package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"monarch-cli/pkg/monarch"
)

// defaultSessionFile is the default JSON session path: on macOS
// ~/Library/Application Support/monarch/session.json (os.UserConfigDir),
// on Linux ~/.config/monarch/session.json.
func defaultSessionFile() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "monarch", "session.json")
}

// legacySessionCandidates lists every path an older setup may have written
// a session to — both the platform default and the XDG-style path older
// docs referenced.
func legacySessionCandidates() []string {
	seen := map[string]bool{}
	var out []string
	add := func(p string) {
		if p != "" && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	add(defaultSessionFile())
	if home, err := os.UserHomeDir(); err == nil {
		add(filepath.Join(home, ".config", "monarch", "session.json"))
	}
	return out
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
			// Never silently downgrade the token to a plaintext file:
			// storage weaker than the Keychain must be an explicit choice.
			return nil, fmt.Errorf("%w — refusing to fall back to a plaintext session file; unlock the login keychain, or set MONARCH_SESSION_FILE to explicitly opt into file storage", err)
		}
		for _, legacy := range legacySessionCandidates() {
			migrateLegacySession(store, legacy)
		}
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

// envOptions maps optional environment overrides to client options:
// MONARCH_DEVICE_UUID (pin the device identity a token was issued to),
// MONARCH_CLIENT_VERSION, MONARCH_USER_AGENT.
func envOptions() []monarch.Option {
	var opts []monarch.Option
	if v := os.Getenv("MONARCH_DEVICE_UUID"); v != "" {
		opts = append(opts, monarch.WithDeviceUUID(v))
	}
	if v := os.Getenv("MONARCH_CLIENT_VERSION"); v != "" {
		opts = append(opts, monarch.WithClientVersion(v))
	}
	if v := os.Getenv("MONARCH_USER_AGENT"); v != "" {
		opts = append(opts, monarch.WithUserAgent(v))
	}
	return opts
}

// newClient builds an authenticated client. Auth resolution order:
//  1. MONARCH_TOKEN env var (direct bearer token)
//  2. saved session (Keychain on macOS, session file elsewhere)
func newClient(extra ...monarch.Option) (*monarch.Client, error) {
	extra = append(envOptions(), extra...)
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
	return monarch.New(append([]monarch.Option{monarch.WithSessionStore(store)}, envOptions()...)...)
}

// newClientMaybeLoggedIn builds a client for the MCP server that MAY be
// unauthenticated: unlike newClient it does not fail when no session
// exists, because the server can recover one interactively via the
// monarch_login elicitation tool. Reads/writes return ErrNotLoggedIn until
// then.
func newClientMaybeLoggedIn(extra ...monarch.Option) (*monarch.Client, error) {
	extra = append(envOptions(), extra...)
	if tok := os.Getenv("MONARCH_TOKEN"); tok != "" {
		return monarch.New(append([]monarch.Option{monarch.WithToken(tok)}, extra...)...)
	}
	store, err := sessionStore()
	if err != nil {
		return nil, err
	}
	return monarch.New(append([]monarch.Option{monarch.WithSessionStore(store)}, extra...)...)
}
