//go:build !darwin

package monarch

import "errors"

// NewKeychainStore is only available on macOS; other platforms use
// NewFileStore.
func NewKeychainStore() (SessionStore, error) {
	return nil, errors.New("monarch: keychain storage is only available on macOS")
}
