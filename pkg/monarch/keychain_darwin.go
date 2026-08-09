//go:build darwin

package monarch

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

const (
	// Absolute path: a credential tool must not be resolvable via $PATH.
	securityBin     = "/usr/bin/security"
	keychainService = "monarch-cli"
	keychainAccount = "session"
	// security(1) exit status for errSecItemNotFound.
	exitItemNotFound = 44
)

// keychainStore keeps the session as a generic password in the user's login
// keychain (encrypted at rest, unlocked with the login session).
//
// Saving uses `security -i` with the add-generic-password command written to
// STDIN, so the secret never appears in argv where `ps` could see it. The
// payload is base64(JSON): base64's alphabet needs no quoting in security's
// interactive tokenizer, and -w's output stays printable ASCII on load.
type keychainStore struct {
	// run is injectable for tests; the default executes securityBin.
	run func(stdin string, args ...string) (stdout string, exitCode int, err error)
}

func NewKeychainStore() (SessionStore, error) {
	return &keychainStore{run: runSecurity}, nil
}

func runSecurity(stdin string, args ...string) (string, int, error) {
	cmd := exec.Command(securityBin, args...)
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return string(out), exitErr.ExitCode(), nil
		}
		return "", 0, err
	}
	return string(out), 0, nil
}

func (k *keychainStore) Save(s *Session) error {
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	payload := base64.StdEncoding.EncodeToString(data)
	// -U upserts, avoiding a delete-then-add race and duplicate-item errors.
	cmdLine := fmt.Sprintf("add-generic-password -U -s %s -a %s -w %s\n", keychainService, keychainAccount, payload)
	_, code, err := k.run(cmdLine, "-i")
	if err != nil {
		return fmt.Errorf("monarch: keychain save: %w", err)
	}
	if code != 0 {
		return fmt.Errorf("monarch: keychain save: security exited %d", code)
	}
	return nil
}

func (k *keychainStore) Load() (*Session, error) {
	out, code, err := k.run("", "find-generic-password", "-s", keychainService, "-a", keychainAccount, "-w")
	if err != nil {
		return nil, fmt.Errorf("monarch: keychain load: %w", err)
	}
	if code == exitItemNotFound {
		return nil, ErrNoSession
	}
	if code != 0 {
		return nil, fmt.Errorf("monarch: keychain load: security exited %d", code)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(out))
	if err != nil {
		return nil, fmt.Errorf("monarch: keychain load: corrupt payload: %w", err)
	}
	var s Session
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("monarch: keychain load: corrupt session: %w", err)
	}
	if s.Token == "" {
		return nil, ErrNoSession
	}
	return &s, nil
}

func (k *keychainStore) Delete() error {
	_, code, err := k.run("", "delete-generic-password", "-s", keychainService, "-a", keychainAccount)
	if err != nil {
		return fmt.Errorf("monarch: keychain delete: %w", err)
	}
	if code != 0 && code != exitItemNotFound {
		return fmt.Errorf("monarch: keychain delete: security exited %d", code)
	}
	return nil
}
