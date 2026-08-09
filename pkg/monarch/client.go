// Package monarch is a from-scratch client for Monarch Money's unofficial
// GraphQL API — the same API the web app uses. It has zero third-party
// dependencies, contacts only the configured base URL, and contains no
// telemetry of any kind.
package monarch

import (
	"crypto/tls"
	"errors"
	"net/http"
	"time"
)

type Client struct {
	httpClient *http.Client
	baseURL    string
	retryBase  time.Duration
	session    *Session
	store      SessionStore
	writesOK   bool

	Auth *AuthService
}

type Option func(*Client)

// WithToken authenticates with a bearer token directly (in-memory session,
// nothing persisted).
func WithToken(tok string) Option {
	return func(c *Client) { c.session = &Session{Token: tok} }
}

// WithSessionStore loads the session from (and persists login results to)
// the given store. A missing session is not an error — the client can still
// log in.
func WithSessionStore(s SessionStore) Option {
	return func(c *Client) { c.store = s }
}

func WithBaseURL(u string) Option {
	return func(c *Client) { c.baseURL = u }
}

func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) { c.httpClient = h }
}

func WithTimeout(d time.Duration) Option {
	return func(c *Client) { c.httpClient.Timeout = d }
}

// WithWritesEnabled arms the mutation methods in writes.go. Without it every
// write returns ErrWritesDisabled. Deliberately explicit: a client built for
// reading can never mutate financial data by accident.
func WithWritesEnabled() Option {
	return func(c *Client) { c.writesOK = true }
}

func New(opts ...Option) (*Client, error) {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	c := &Client{
		baseURL:   DefaultBaseURL,
		retryBase: 500 * time.Millisecond,
		httpClient: &http.Client{
			Timeout:   30 * time.Second,
			Transport: tr,
		},
	}
	for _, opt := range opts {
		opt(c)
	}
	if c.session == nil && c.store != nil {
		s, err := c.store.Load()
		switch {
		case err == nil:
			c.session = s
		case errors.Is(err, ErrNoSession):
			// Not logged in yet; Auth.Login* can fix that.
		default:
			return nil, err
		}
	}
	c.Auth = &AuthService{c: c}
	return c, nil
}

// Session returns the current session, or nil when unauthenticated.
func (c *Client) Session() *Session { return c.session }

// SaveSession persists the current session to the configured store.
func (c *Client) SaveSession() error {
	if c.store == nil {
		return errors.New("monarch: no session store configured")
	}
	if c.session == nil {
		return ErrNoSession
	}
	return c.store.Save(c.session)
}
