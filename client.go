package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/eshaffer321/monarch-go/v2/pkg/monarch"
)

// sessionPath returns the path where the login session is persisted.
// Override with MONARCH_SESSION_FILE.
func sessionPath() string {
	if p := os.Getenv("MONARCH_SESSION_FILE"); p != "" {
		return p
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "monarch", "session.json")
}

// newClient builds a Monarch client. Auth resolution order:
//  1. MONARCH_TOKEN env var (direct bearer token)
//  2. saved session file from `monarch login`
func newClient() (*monarch.Client, error) {
	opts := &monarch.ClientOptions{
		Timeout: 30 * time.Second,
	}
	if tok := os.Getenv("MONARCH_TOKEN"); tok != "" {
		opts.Token = tok
	} else {
		sp := sessionPath()
		if _, err := os.Stat(sp); err != nil {
			return nil, fmt.Errorf("not logged in: run `monarch login` first (or set MONARCH_TOKEN)")
		}
		opts.SessionFile = sp
	}
	return monarch.NewClient(opts)
}

// newClientForLogin builds an unauthenticated client that will persist
// its session to the session file on successful login.
func newClientForLogin() (*monarch.Client, error) {
	sp := sessionPath()
	if err := os.MkdirAll(filepath.Dir(sp), 0o700); err != nil {
		return nil, err
	}
	return monarch.NewClient(&monarch.ClientOptions{
		Timeout:     30 * time.Second,
		SessionFile: sp,
	})
}
