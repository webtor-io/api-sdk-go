package webtor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Version of the SDK, used in the default User-Agent.
const Version = "0.1.0"

// Client talks to a webtor API backend. Construct with New; the zero value is
// not usable. All methods are safe for concurrent use.
type Client struct {
	backend Backend
	hc      *http.Client
	retry   RetryPolicy
	ua      string
	logger  *slog.Logger
}

// Option configures a Client.
type Option func(*Client)

// WithHTTPClient replaces the underlying *http.Client. The client's Timeout
// is left untouched; per-call deadlines come from the context and from the
// SDK's own long-call overrides (AddResource).
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.hc = h } }

// WithRetry replaces the default retry policy.
func WithRetry(p RetryPolicy) Option { return func(c *Client) { c.retry = p } }

// WithUserAgent replaces the default User-Agent header.
func WithUserAgent(ua string) Option { return func(c *Client) { c.ua = ua } }

// WithLogger enables debug logging of requests and retries.
func WithLogger(l *slog.Logger) Option { return func(c *Client) { c.logger = l } }

// New returns a Client for the given backend.
func New(b Backend, opts ...Option) (*Client, error) {
	if b == nil {
		return nil, fmt.Errorf("webtor: backend must not be nil")
	}
	c := &Client{
		backend: b,
		hc:      &http.Client{},
		retry:   DefaultRetryPolicy(),
		ua:      "webtor-sdk-go/" + Version,
	}
	for _, o := range opts {
		o(c)
	}
	return c, nil
}

// Backend returns the backend the client was constructed with.
func (c *Client) Backend() Backend { return c.backend }

// Supports reports whether the configured backend has every given capability.
func (c *Client) Supports(caps Capabilities) bool {
	return c.backend.Capabilities()&caps == caps
}

func (c *Client) require(caps Capabilities) error {
	if !c.Supports(caps) {
		return &CapabilityError{Backend: c.backend.Kind(), Capability: caps}
	}
	return nil
}

// apiRequest describes one API call for do. Bodies are byte slices, not
// readers, so retried attempts can resend them.
type apiRequest struct {
	method      string
	path        string // relative to the backend base URL, no leading slash
	query       url.Values
	body        []byte
	contentType string
	// timeout, when non-zero, bounds the whole call (all attempts) unless the
	// caller's context has an earlier deadline.
	timeout time.Duration
	// noRetry disables retries entirely (single-shot calls like the device
	// token poll, which has its own pacing protocol).
	noRetry bool
}

// do performs the request with auth, retry and error normalization, decoding
// a 2xx JSON body into out (skipped when out is nil).
func (c *Client) do(ctx context.Context, req apiRequest, out any) error {
	body, err := c.doRaw(ctx, req)
	if err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("webtor: decoding %s %s response: %w", req.method, req.path, err)
	}
	return nil
}

// doRaw performs the request and returns the raw 2xx body.
func (c *Client) doRaw(ctx context.Context, req apiRequest) ([]byte, error) {
	if req.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, req.timeout)
		defer cancel()
	}

	u := *c.backend.BaseURL()
	u.Path = u.Path + "/" + strings.TrimPrefix(req.path, "/")
	if len(req.query) > 0 {
		u.RawQuery = req.query.Encode()
	}

	attempts := c.retry.MaxAttempts
	if req.noRetry || attempts < 1 {
		attempts = 1
	}
	var lastErr error
	for attempt := 1; ; attempt++ {
		hreq, err := http.NewRequestWithContext(ctx, req.method, u.String(), bytes.NewReader(req.body))
		if err != nil {
			return nil, fmt.Errorf("webtor: building request: %w", err)
		}
		if req.contentType != "" {
			hreq.Header.Set("Content-Type", req.contentType)
		}
		hreq.Header.Set("User-Agent", c.ua)
		hreq.Header.Set("Accept", "application/json")
		c.backend.Authorize(hreq)

		body, err := c.attempt(hreq)
		if err == nil {
			return body, nil
		}
		lastErr = err

		wait, retryable := c.retry.shouldRetry(req.method, err)
		if !retryable || attempt >= attempts {
			return nil, err
		}
		if c.logger != nil {
			c.logger.Debug("webtor: retrying", "method", req.method, "path", req.path,
				"attempt", attempt, "wait", wait, "err", err)
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("webtor: %w (last error: %v)", ctx.Err(), lastErr)
		case <-time.After(wait):
		}
	}
}

func (c *Client) attempt(hreq *http.Request) ([]byte, error) {
	resp, err := c.hc.Do(hreq)
	if err != nil {
		return nil, &transportError{err: err}
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, &transportError{err: err}
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, decodeError(resp.StatusCode, body, resp.Header)
	}
	return body, nil
}

// transportError wraps network-level failures (connection reset, DNS, TLS) so
// the retry policy can tell them apart from API errors.
type transportError struct{ err error }

func (e *transportError) Error() string { return "webtor: transport: " + e.err.Error() }
func (e *transportError) Unwrap() error { return e.err }
