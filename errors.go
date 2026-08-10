package webtor

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// Error codes. The web-ui backend sends them verbatim; for the other dialects
// (rest-api's bare string, RapidAPI's {"message"} and empty bodies) the code
// is synthesized from the HTTP status, so callers can always branch on Code
// regardless of the backend.
const (
	CodeBadRequest           = "bad_request"
	CodeUnauthorized         = "unauthorized"
	CodeForbidden            = "forbidden"
	CodePaymentRequired      = "payment_required"
	CodeNotFound             = "not_found"
	CodeConflict             = "conflict"
	CodeMethodNotAllowed     = "method_not_allowed"
	CodeRateLimited          = "rate_limited"
	CodeAuthorizationPending = "authorization_pending"
	CodeSlowDown             = "slow_down"
	CodeExpiredToken         = "expired_token"
	CodeUnavailable          = "unavailable"
	CodeInternal             = "internal_error"
	CodeUpstream             = "upstream_error"
	CodeUpstreamTimeout      = "upstream_timeout"
)

// Error is an API error normalized across the three backend dialects.
type Error struct {
	// HTTPStatus is the response status code.
	HTTPStatus int
	// Code is a stable machine-readable code (see the Code* constants).
	// Always populated: synthesized from HTTPStatus when the backend's error
	// dialect does not carry one.
	Code string
	// Message is the human-readable message, when the backend sent one.
	Message string
	// RetryAfter is the server-requested pause before retrying; non-zero only
	// on rate-limited responses that carried a Retry-After header.
	RetryAfter time.Duration
}

func (e *Error) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("webtor: %s (http %d)", e.Code, e.HTTPStatus)
	}
	return fmt.Sprintf("webtor: %s: %s", e.Code, e.Message)
}

// IsNotFound reports whether err is an API error with code not_found.
func IsNotFound(err error) bool { return hasCode(err, CodeNotFound) }

// IsUnauthorized reports whether err is an API error with code unauthorized.
func IsUnauthorized(err error) bool { return hasCode(err, CodeUnauthorized) }

// IsForbidden reports whether err is an API error with code forbidden.
func IsForbidden(err error) bool { return hasCode(err, CodeForbidden) }

// IsPaymentRequired reports whether err is an API error with code
// payment_required (free tier hitting a paid-only API).
func IsPaymentRequired(err error) bool { return hasCode(err, CodePaymentRequired) }

// IsConflict reports whether err is an API error with code conflict.
func IsConflict(err error) bool { return hasCode(err, CodeConflict) }

// IsRateLimited reports whether err is an API error with code rate_limited.
func IsRateLimited(err error) bool { return hasCode(err, CodeRateLimited) }

func hasCode(err error, code string) bool {
	var ae *Error
	return errors.As(err, &ae) && ae.Code == code
}

// webUIErrorEnvelope is web-ui's error dialect: {"error":{"code","message"}}.
type webUIErrorEnvelope struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// decodeError normalizes a non-2xx response body into *Error. It recognizes,
// in order: the web-ui envelope, rest-api's {"error":"string"}, RapidAPI's
// own {"message":"string"}, and anything else (empty 403 from the gateway,
// HTML from an intermediary) by synthesizing the code from the status.
func decodeError(status int, body []byte, hdr http.Header) *Error {
	e := &Error{HTTPStatus: status, RetryAfter: retryAfter(hdr)}

	var env webUIErrorEnvelope
	if err := json.Unmarshal(body, &env); err == nil && env.Error.Code != "" {
		e.Code = env.Error.Code
		e.Message = env.Error.Message
		return e
	}
	var str struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &str); err == nil && str.Error != "" {
		e.Message = str.Error
		e.Code = codeFromStatus(status)
		return e
	}
	var msg struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &msg); err == nil && msg.Message != "" {
		e.Message = msg.Message
		e.Code = codeFromStatus(status)
		return e
	}
	e.Code = codeFromStatus(status)
	return e
}

func codeFromStatus(status int) string {
	switch status {
	case http.StatusBadRequest, http.StatusRequestEntityTooLarge:
		return CodeBadRequest
	case http.StatusUnauthorized:
		return CodeUnauthorized
	case http.StatusPaymentRequired:
		return CodePaymentRequired
	case http.StatusForbidden:
		return CodeForbidden
	case http.StatusNotFound:
		return CodeNotFound
	case http.StatusMethodNotAllowed:
		return CodeMethodNotAllowed
	case http.StatusRequestTimeout:
		return CodeUpstreamTimeout
	case http.StatusConflict:
		return CodeConflict
	case http.StatusTooManyRequests:
		return CodeRateLimited
	case http.StatusBadGateway, http.StatusGatewayTimeout:
		return CodeUpstream
	case http.StatusServiceUnavailable:
		return CodeUnavailable
	default:
		return CodeInternal
	}
}

func retryAfter(hdr http.Header) time.Duration {
	v := hdr.Get("Retry-After")
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}
