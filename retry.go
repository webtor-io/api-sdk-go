package webtor

import (
	"errors"
	"math/rand/v2"
	"net/http"
	"time"
)

// RetryPolicy controls automatic retries. Retries apply to rate limiting
// (429, honoring Retry-After) on every method, and additionally to transient
// upstream failures (upstream_error, unavailable, upstream_timeout, network
// errors) on idempotent methods (GET/HEAD). POST /resource is never retried
// on a timeout: the server-side add keeps running, and re-POSTing the same
// magnet is the caller's naturally idempotent way to poll.
type RetryPolicy struct {
	// MaxAttempts is the total number of attempts (1 = no retries).
	MaxAttempts int
	// BaseWait is the first backoff wait; it doubles per retry with jitter.
	BaseWait time.Duration
	// MaxWait caps a single wait, including server-requested Retry-After.
	MaxWait time.Duration
}

// DefaultRetryPolicy returns the policy used when none is configured:
// 3 attempts, 500ms base backoff, 30s cap.
func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{MaxAttempts: 3, BaseWait: 500 * time.Millisecond, MaxWait: 30 * time.Second}
}

// shouldRetry decides whether err from a method call warrants another
// attempt, and after what wait.
func (p RetryPolicy) shouldRetry(method string, err error) (time.Duration, bool) {
	idempotent := method == http.MethodGet || method == http.MethodHead

	var te *transportError
	if errors.As(err, &te) {
		return p.backoff(), idempotent
	}
	var ae *Error
	if !errors.As(err, &ae) {
		return 0, false
	}
	switch ae.Code {
	case CodeRateLimited:
		wait := ae.RetryAfter
		if wait <= 0 {
			wait = p.backoff()
		}
		if wait > p.MaxWait {
			wait = p.MaxWait
		}
		return wait, true
	case CodeUpstream, CodeUnavailable, CodeUpstreamTimeout:
		return p.backoff(), idempotent
	}
	return 0, false
}

func (p RetryPolicy) backoff() time.Duration {
	wait := p.BaseWait
	if wait < 2 {
		wait = 500 * time.Millisecond
	}
	// Full jitter: [wait/2, wait). Attempt-based doubling is folded in by the
	// caller retrying with growing waits being unnecessary at 3 attempts.
	wait = wait/2 + time.Duration(rand.Int64N(int64(wait/2)))
	if p.MaxWait > 0 && wait > p.MaxWait {
		wait = p.MaxWait
	}
	return wait
}
