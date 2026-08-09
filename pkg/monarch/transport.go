package monarch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"strconv"
	"time"
)

const (
	// DefaultBaseURL is the current Monarch API host (api.monarchmoney.com
	// is the old, dead one).
	DefaultBaseURL = "https://api.monarch.com"

	graphqlPath = "/graphql"
	loginPath   = "/auth/login/" // trailing slash matters

	userAgent = "monarch-cli/1.0"

	maxAttempts   = 4
	maxRetryDelay = 8 * time.Second
	maxBodyBytes  = 4 << 20 // defensive cap on response reads
	errBodyKeep   = 500     // bytes of an error body retained on APIError
)

type gqlRequest struct {
	OperationName string         `json:"operationName"`
	Query         string         `json:"query"`
	Variables     map[string]any `json:"variables,omitempty"`
}

// doGraphQL executes one GraphQL operation and decodes the response's data
// field into out. Queries are read-only and therefore safe to retry.
func (c *Client) doGraphQL(ctx context.Context, operation, query string, variables map[string]any, out any) error {
	return c.graphQL(ctx, operation, query, variables, out, true)
}

// doGraphQLNoRetry is for mutations: a single attempt, so a write can never
// double-fire on an ambiguous network failure.
func (c *Client) doGraphQLNoRetry(ctx context.Context, operation, query string, variables map[string]any, out any) error {
	return c.graphQL(ctx, operation, query, variables, out, false)
}

func (c *Client) graphQL(ctx context.Context, operation, query string, variables map[string]any, out any, retry bool) error {
	if c.session == nil || c.session.Token == "" {
		return ErrNotLoggedIn
	}
	payload, err := json.Marshal(gqlRequest{OperationName: operation, Query: query, Variables: variables})
	if err != nil {
		return fmt.Errorf("monarch: %s: encode request: %w", operation, err)
	}
	build := func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+graphqlPath, bytes.NewReader(payload))
		if err != nil {
			return nil, err
		}
		c.setCommonHeaders(req.Header, c.session.DeviceUUID)
		req.Header.Set("Authorization", "Token "+c.session.Token)
		return req, nil
	}
	status, body, err := c.do(ctx, operation, build, retry)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return apiError(operation, status, body)
	}
	var resp struct {
		Data   json.RawMessage `json:"data"`
		Errors []GQLErrorItem  `json:"errors"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("monarch: %s: decode response: %w", operation, err)
	}
	if len(resp.Errors) > 0 {
		return &GraphQLError{Operation: operation, Errors: resp.Errors}
	}
	if out != nil {
		if err := json.Unmarshal(resp.Data, out); err != nil {
			return fmt.Errorf("monarch: %s: decode data: %w", operation, err)
		}
	}
	return nil
}

// setCommonHeaders applies the header set Monarch requires; login without
// Client-Platform and device-uuid 404s (hammem/monarchmoney#156).
func (c *Client) setCommonHeaders(h http.Header, deviceUUID string) {
	h.Set("Content-Type", "application/json")
	h.Set("Accept", "application/json")
	h.Set("Client-Platform", "web")
	h.Set("Origin", "https://app.monarch.com")
	h.Set("Referer", "https://app.monarch.com/")
	h.Set("User-Agent", userAgent)
	if deviceUUID != "" {
		h.Set("device-uuid", deviceUUID)
	}
}

// do executes a request with the retry policy: network errors, 429 and 5xx
// retry (when retry is true) with exponential backoff, ±25% jitter, and
// Retry-After honored on 429. Other statuses return to the caller.
func (c *Client) do(ctx context.Context, operation string, build func() (*http.Request, error), retry bool) (int, []byte, error) {
	attempts := 1
	if retry {
		attempts = maxAttempts
	}
	var lastErr error
	var retryAfter time.Duration
	for attempt := range attempts {
		if attempt > 0 {
			select {
			case <-time.After(c.backoff(attempt, retryAfter)):
			case <-ctx.Done():
				return 0, nil, ctx.Err()
			}
		}
		req, err := build()
		if err != nil {
			return 0, nil, fmt.Errorf("monarch: %s: %w", operation, err)
		}
		resp, err := c.httpClient.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return 0, nil, ctx.Err()
			}
			lastErr = fmt.Errorf("monarch: %s: %w", operation, err)
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
		resp.Body.Close()
		if readErr != nil {
			lastErr = fmt.Errorf("monarch: %s: read response: %w", operation, readErr)
			continue
		}
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			lastErr = apiError(operation, resp.StatusCode, body)
			retryAfter = parseRetryAfter(resp.Header)
			continue
		}
		return resp.StatusCode, body, nil
	}
	return 0, nil, lastErr
}

// backoff computes the delay before the attempt-th retry (1-based):
// retryBase·2^(attempt−1), capped, jittered ±25%, floored by Retry-After.
func (c *Client) backoff(attempt int, retryAfter time.Duration) time.Duration {
	d := min(c.retryBase<<(attempt-1), maxRetryDelay)
	d = time.Duration(float64(d) * (1 + (rand.Float64()-0.5)/2))
	return max(d, retryAfter)
}

func parseRetryAfter(h http.Header) time.Duration {
	secs, err := strconv.Atoi(h.Get("Retry-After"))
	if err != nil || secs < 0 {
		return 0
	}
	return min(time.Duration(secs)*time.Second, maxRetryDelay)
}

func apiError(operation string, status int, body []byte) error {
	e := &APIError{StatusCode: status, Operation: operation, Body: string(body[:min(len(body), errBodyKeep)])}
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		e.wrapped = ErrSessionExpired
	case http.StatusTooManyRequests:
		e.wrapped = ErrRateLimited
	}
	return e
}
