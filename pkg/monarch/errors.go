package monarch

import (
	"errors"
	"fmt"
	"strings"
)

// Sentinel errors. Callers branch with errors.Is — never by matching error text.
var (
	ErrNotLoggedIn      = errors.New("monarch: not logged in")
	ErrSessionExpired   = errors.New("monarch: session expired or rejected — run `monarch login`")
	ErrMFARequired      = errors.New("monarch: MFA code required")
	ErrEmailOTPRequired = errors.New("monarch: email OTP required")
	ErrInvalidLogin     = errors.New("monarch: invalid email or password")
	ErrRateLimited      = errors.New("monarch: rate limited")
	ErrNoSession        = errors.New("monarch: no saved session")
	ErrWritesDisabled   = errors.New("monarch: write operations are disabled on this client")
)

// APIError is a non-2xx HTTP response from the Monarch API.
//
// Error() deliberately reports only the operation and status code: raw server
// output must never reach logs or terminals. The truncated body is unexported
// so no format verb (%+v, %#v) or reflective logger can surface it by
// accident; call DebugBody to opt in.
type APIError struct {
	StatusCode int
	Operation  string
	body       string
	wrapped    error
}

func (e *APIError) Error() string {
	return fmt.Sprintf("monarch: %s: HTTP %d", e.Operation, e.StatusCode)
}

func (e *APIError) Unwrap() error { return e.wrapped }

// DebugBody returns the first 500 bytes of the response body for
// programmatic inspection. Do not log it.
func (e *APIError) DebugBody() string { return e.body }

// GraphQLError is an HTTP 200 response carrying a non-empty errors array.
type GraphQLError struct {
	Operation string
	Errors    []GQLErrorItem
}

type GQLErrorItem struct {
	Message string `json:"message"`
	Path    []any  `json:"path,omitempty"`
}

func (e *GraphQLError) Error() string {
	msgs := make([]string, len(e.Errors))
	for i, item := range e.Errors {
		msgs[i] = item.Message
	}
	return fmt.Sprintf("monarch: %s: %s", e.Operation, strings.Join(msgs, "; "))
}
