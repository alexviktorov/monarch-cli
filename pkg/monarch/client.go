// Package monarch is a from-scratch client for Monarch Money's unofficial
// GraphQL API — the same API the web app uses. It has zero third-party
// dependencies, contacts only the configured base URL, and contains no
// telemetry of any kind.
package monarch

import (
	"context"
	"crypto/tls"
	"errors"
	"net/http"
	"time"
)

// Concurrency contract: a Client is safe for concurrent use of the read and
// write service methods (they only read the session). Auth.Login* and
// SaveSession swap/persist the session and must not run concurrently with
// other calls — the CLI and MCP binaries never do so (logins are exclusively
// interactive), and logins themselves are serialized internally.
type Client struct {
	httpClient    *http.Client
	baseURL       string
	retryBase     time.Duration
	session       *Session
	store         SessionStore
	writesOK      bool
	deviceUUID    string // explicit override (WithDeviceUUID)
	userAgent     string
	clientVersion string

	Auth         *AuthService
	Accounts     *AccountsService
	Transactions *TransactionsService
	Categories   *CategoriesService
	Budgets      *BudgetsService
	Cashflow     *CashflowService
	Recurring    *RecurringService
	Tags         *TagsService
	Holdings     *HoldingsService
	Rules        *RulesService
	Goals        *GoalsService
	Institutions *InstitutionsService
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

// WithDeviceUUID pins the device-uuid header, overriding both a stored
// session's UUID and the token-derived default. Use it to present the same
// device identity as the browser a token was extracted from.
func WithDeviceUUID(uuid string) Option {
	return func(c *Client) { c.deviceUUID = uuid }
}

// WithUserAgent overrides the User-Agent header (escape hatch in case
// Monarch ever tightens UA filtering).
func WithUserAgent(ua string) Option {
	return func(c *Client) { c.userAgent = ua }
}

// WithClientVersion overrides the monarch-client-version header. Monarch
// validates the client version against a server-side minimum; see
// UPSTREAM.md for the recapture procedure when the default goes stale.
func WithClientVersion(v string) Option {
	return func(c *Client) { c.clientVersion = v }
}

func New(opts ...Option) (*Client, error) {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	c := &Client{
		baseURL:       DefaultBaseURL,
		retryBase:     500 * time.Millisecond,
		userAgent:     defaultUserAgent,
		clientVersion: defaultClientVersion,
		httpClient: &http.Client{
			Timeout:   30 * time.Second,
			Transport: tr,
			// A redirect from this API is always a stop signal; following
			// one silently turns into a confusing decode error.
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
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
	// Monarch expects a device-uuid on every request and ties tokens to a
	// device identity. An explicit override wins; otherwise a session
	// without a stored UUID (the WithToken path) gets one derived
	// deterministically from the token, stable across invocations.
	if c.session != nil {
		if c.deviceUUID != "" {
			c.session.DeviceUUID = c.deviceUUID
		} else if c.session.DeviceUUID == "" {
			c.session.DeviceUUID = deviceUUIDFromToken(c.session.Token)
		}
	}
	c.Auth = &AuthService{c: c}
	c.Accounts = &AccountsService{c: c}
	c.Transactions = &TransactionsService{c: c}
	c.Categories = &CategoriesService{c: c}
	c.Budgets = &BudgetsService{c: c}
	c.Cashflow = &CashflowService{c: c}
	c.Recurring = &RecurringService{c: c}
	c.Tags = &TagsService{c: c}
	c.Holdings = &HoldingsService{c: c}
	c.Rules = &RulesService{c: c}
	c.Goals = &GoalsService{c: c}
	c.Institutions = &InstitutionsService{c: c}
	return c, nil
}

const queryGetIdentity = `query GetIdentity {
  me {
    id
    email
  }
}`

type Identity struct {
	ID    string `json:"id"`
	Email string `json:"email"`
}

// Ping performs a minimal authenticated round-trip, confirming the session
// is valid server-side (a local session can look fine while the token is
// long dead).
func (c *Client) Ping(ctx context.Context) (*Identity, error) {
	var out struct {
		Me *Identity `json:"me"`
	}
	if err := c.doGraphQL(ctx, "GetIdentity", queryGetIdentity, nil, &out); err != nil {
		return nil, err
	}
	if out.Me == nil || out.Me.ID == "" {
		return nil, errors.New("monarch: GetIdentity: empty response")
	}
	return out.Me, nil
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
