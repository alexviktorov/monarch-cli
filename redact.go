package main

import (
	"context"
	"errors"
	"regexp"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"monarch-cli/pkg/monarch"
)

// redactErr is the single choke point where handler errors cross into MCP
// responses. The library contract already keeps tokens and raw bodies out
// of error strings; this is a regex backstop, plus a fixed, actionable
// message for auth failures (never anything that could echo a credential).
// The optional `token\s+` swallows header schemes ("Authorization: Token
// <value>", "Bearer <value>") so the value itself is what gets redacted.
var redactPattern = regexp.MustCompile(`(?i)\b(authorization|cookie|password|secret|token)\b[=:\s]+(?:(?:token|bearer)\s+)?\S+`)

func redactErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, monarch.ErrSessionExpired) || errors.Is(err, monarch.ErrNotLoggedIn) || errors.Is(err, monarch.ErrNoSession) {
		return errors.New("Monarch session expired or missing. Run `monarch login` in a terminal, then retry.")
	}
	return errors.New(redactPattern.ReplaceAllString(err.Error(), "$1 [redacted]"))
}

// tokenBucket is a tiny stdlib rate limiter (no x/time dependency).
type tokenBucket struct {
	mu     sync.Mutex
	tokens float64
	last   time.Time
	rate   float64 // tokens per second
	burst  float64
}

func newTokenBucket(perMinute, burst float64) *tokenBucket {
	return &tokenBucket{tokens: burst, last: time.Now(), rate: perMinute / 60, burst: burst}
}

func (b *tokenBucket) allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	b.tokens = min(b.burst, b.tokens+now.Sub(b.last).Seconds()*b.rate)
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// toolLimits guards every tool call: rate limit, then concurrency cap,
// then a per-call timeout. Runaway-LLM protection for a live financial API.
type toolLimits struct {
	bucket  *tokenBucket
	sem     chan struct{}
	timeout time.Duration
}

func newToolLimits() *toolLimits {
	return &toolLimits{
		bucket:  newTokenBucket(30, 10), // 30 calls/min, burst of 10
		sem:     make(chan struct{}, 3), // 3 concurrent Monarch calls
		timeout: 30 * time.Second,
	}
}

// wrap applies the shared middleware to a typed tool handler. Failing fast
// on rate limits (rather than queueing) teaches the calling model to stop;
// a queue would teach it to pile on.
func wrap[In, Out any](l *toolLimits, h mcp.ToolHandlerFor[In, Out]) mcp.ToolHandlerFor[In, Out] {
	return func(ctx context.Context, req *mcp.CallToolRequest, in In) (*mcp.CallToolResult, Out, error) {
		var zero Out
		if !l.bucket.allow() {
			return nil, zero, errors.New("rate limited: too many Monarch calls; wait a minute before retrying")
		}
		select {
		case l.sem <- struct{}{}:
			defer func() { <-l.sem }()
		default:
			return nil, zero, errors.New("too many concurrent Monarch calls; retry in a moment")
		}
		ctx, cancel := context.WithTimeout(ctx, l.timeout)
		defer cancel()
		res, out, err := h(ctx, req, in)
		if err != nil {
			return nil, zero, redactErr(err)
		}
		return res, out, nil
	}
}
