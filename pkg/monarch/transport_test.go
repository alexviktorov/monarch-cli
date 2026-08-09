package monarch

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func testClient(t *testing.T, handler http.Handler, opts ...Option) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	opts = append([]Option{WithBaseURL(srv.URL)}, opts...)
	c, err := New(opts...)
	if err != nil {
		t.Fatal(err)
	}
	c.retryBase = time.Millisecond
	return c
}

func authedClient(t *testing.T, handler http.Handler) *Client {
	t.Helper()
	c := testClient(t, handler, WithToken("test-token"))
	c.session.DeviceUUID = "test-device-uuid"
	return c
}

func TestGraphQLRequestShape(t *testing.T) {
	var got struct {
		method, path, auth, platform, device, ua string
		mclient, mversion                        string
		body                                     gqlRequest
	}
	c := authedClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.method, got.path = r.Method, r.URL.Path
		got.auth = r.Header.Get("Authorization")
		got.platform = r.Header.Get("Client-Platform")
		got.device = r.Header.Get("device-uuid")
		got.ua = r.Header.Get("User-Agent")
		got.mclient = r.Header.Get("monarch-client")
		got.mversion = r.Header.Get("monarch-client-version")
		if err := json.NewDecoder(r.Body).Decode(&got.body); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		w.Write([]byte(`{"data":{}}`))
	}))
	err := c.doGraphQL(context.Background(), "TestOp", "query TestOp { x }", map[string]any{"a": 1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.method != http.MethodPost || got.path != "/graphql" {
		t.Errorf("request = %s %s, want POST /graphql", got.method, got.path)
	}
	if got.auth != "Token test-token" {
		t.Errorf("Authorization = %q, want \"Token test-token\"", got.auth)
	}
	if got.platform != "web" {
		t.Errorf("Client-Platform = %q, want web", got.platform)
	}
	if got.device != "test-device-uuid" {
		t.Errorf("device-uuid = %q", got.device)
	}
	if got.ua != defaultUserAgent {
		t.Errorf("User-Agent = %q, want %q", got.ua, defaultUserAgent)
	}
	if got.mclient != monarchClientName || got.mversion != defaultClientVersion {
		t.Errorf("monarch-client headers = %q/%q, want %q/%q", got.mclient, got.mversion, monarchClientName, defaultClientVersion)
	}
	if got.body.OperationName != "TestOp" || got.body.Query == "" {
		t.Errorf("body = %+v, want operationName TestOp with query", got.body)
	}
}

func TestGraphQLDecodesData(t *testing.T) {
	c := authedClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":{"accounts":[{"id":"1"},{"id":"2"}]}}`))
	}))
	var out struct {
		Accounts []struct{ ID string } `json:"accounts"`
	}
	if err := c.doGraphQL(context.Background(), "Op", "query Op { accounts { id } }", nil, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Accounts) != 2 || out.Accounts[1].ID != "2" {
		t.Errorf("decoded %+v", out)
	}
}

func TestGraphQLErrorArray(t *testing.T) {
	c := authedClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":null,"errors":[{"message":"Not authorized"},{"message":"boom"}]}`))
	}))
	err := c.doGraphQL(context.Background(), "Op", "query Op { x }", nil, nil)
	var gqlErr *GraphQLError
	if !errors.As(err, &gqlErr) {
		t.Fatalf("err = %v, want *GraphQLError", err)
	}
	if len(gqlErr.Errors) != 2 || gqlErr.Operation != "Op" {
		t.Errorf("GraphQLError = %+v", gqlErr)
	}
}

func TestGraphQLUnauthorized(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		c := authedClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
		}))
		err := c.doGraphQL(context.Background(), "Op", "query Op { x }", nil, nil)
		if !errors.Is(err, ErrSessionExpired) {
			t.Errorf("status %d: err = %v, want ErrSessionExpired", status, err)
		}
	}
}

func TestGraphQLRetries429ThenSucceeds(t *testing.T) {
	var calls atomic.Int32
	c := authedClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) <= 2 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Write([]byte(`{"data":{}}`))
	}))
	if err := c.doGraphQL(context.Background(), "Op", "query Op { x }", nil, nil); err != nil {
		t.Fatal(err)
	}
	if n := calls.Load(); n != 3 {
		t.Errorf("calls = %d, want 3", n)
	}
}

func TestGraphQLRetryExhaustion(t *testing.T) {
	var calls atomic.Int32
	c := authedClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	err := c.doGraphQL(context.Background(), "Op", "query Op { x }", nil, nil)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != 500 {
		t.Fatalf("err = %v, want *APIError 500", err)
	}
	if n := calls.Load(); n != maxAttempts {
		t.Errorf("calls = %d, want %d", n, maxAttempts)
	}
}

func TestMutationNeverRetries(t *testing.T) {
	var calls atomic.Int32
	c := authedClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	err := c.doGraphQLNoRetry(context.Background(), "Mut", "mutation Mut { x }", nil, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if n := calls.Load(); n != 1 {
		t.Errorf("calls = %d, want 1 (mutations must not retry)", n)
	}
}

func TestGraphQLContextCancelDuringBackoff(t *testing.T) {
	c := authedClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	c.retryBase = time.Second // force a long backoff after the first 500
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := c.doGraphQL(ctx, "Op", "query Op { x }", nil, nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want DeadlineExceeded", err)
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Errorf("cancellation took %v, backoff did not honor ctx", elapsed)
	}
}

func TestWithTokenDerivesStableDeviceUUID(t *testing.T) {
	// Monarch ties tokens to a device identity: the uuid for a token-only
	// session must be identical across separate client constructions, not
	// merely within one process.
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":{}}`))
	})
	var uuids []string
	for range 2 {
		c := testClient(t, handler, WithToken("tok"))
		uuids = append(uuids, c.session.DeviceUUID)
	}
	if uuids[0] == "" || uuids[0] != uuids[1] {
		t.Errorf("device-uuids across constructions = %v, want identical non-empty", uuids)
	}
	other := testClient(t, handler, WithToken("other-tok"))
	if other.session.DeviceUUID == uuids[0] {
		t.Error("different tokens must not share a device identity")
	}
	pinned := testClient(t, handler, WithToken("tok"), WithDeviceUUID("pin-1234"))
	if pinned.session.DeviceUUID != "pin-1234" {
		t.Errorf("WithDeviceUUID override ignored: %s", pinned.session.DeviceUUID)
	}
}

func TestRedirectsAreNotFollowed(t *testing.T) {
	var hits atomic.Int32
	c := authedClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.Redirect(w, r, "/elsewhere", http.StatusFound)
	}))
	err := c.doGraphQL(context.Background(), "Op", "query Op { x }", nil, nil)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusFound {
		t.Fatalf("err = %v, want APIError 302 (redirects are a stop signal)", err)
	}
	if hits.Load() != 1 {
		t.Errorf("hits = %d, redirect must not be followed", hits.Load())
	}
}

func TestRetryAfterHonoredBeyondBackoffCap(t *testing.T) {
	h := http.Header{}
	h.Set("Retry-After", "30")
	if got := parseRetryAfter(h); got != 30*time.Second {
		t.Errorf("parseRetryAfter(30) = %v, want 30s (must not be capped to the backoff max)", got)
	}
	h.Set("Retry-After", "300")
	if got := parseRetryAfter(h); got != maxRetryAfterWait {
		t.Errorf("parseRetryAfter(300) = %v, want the %v ceiling", got, maxRetryAfterWait)
	}
	c := &Client{retryBase: 500 * time.Millisecond}
	if got := c.backoff(1, 30*time.Second); got != 30*time.Second {
		t.Errorf("backoff with Retry-After 30s = %v, want the server's ask to win", got)
	}
}

func TestGraphQLRequiresToken(t *testing.T) {
	var calls atomic.Int32
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
	}))
	err := c.doGraphQL(context.Background(), "Op", "query Op { x }", nil, nil)
	if !errors.Is(err, ErrNotLoggedIn) {
		t.Fatalf("err = %v, want ErrNotLoggedIn", err)
	}
	if calls.Load() != 0 {
		t.Error("request was sent without a token")
	}
}

func TestAPIErrorHidesBody(t *testing.T) {
	c := authedClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"detail":"account 12345 secret stuff"}`))
	}))
	err := c.doGraphQL(context.Background(), "Op", "query Op { x }", nil, nil)
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %v, want *APIError", err)
	}
	if apiErr.DebugBody() == "" {
		t.Error("DebugBody should retain the truncated payload for inspection")
	}
	if msg := err.Error(); msg != "monarch: Op: HTTP 400" {
		t.Errorf("Error() = %q — raw body must not leak into the message", msg)
	}
}
