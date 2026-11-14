package webtor

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// Error codes. The web-ui backend sends them verbatim; when a body carries no
// code (rest-api's bare {"error"}, RapidAPI's {"message"}, empty bodies) it is
// synthesized from the HTTP status, so callers can always branch on Code
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

// Private torrent codes, sent by rest-api and passed through by the web-ui
// and RapidAPI backends. Error.Reason refines some of them.
const (
	CodePrivateTorrent           = "private_torrent"
	CodePrivateTorrentFile       = "private_torrent_file"
	CodePrivateNeedsAccount      = "private_needs_account"
	CodePrivateNoTracker         = "private_no_tracker"
	CodePrivateTooManyTrackers   = "private_too_many_trackers"
	CodePrivateMagnet            = "private_magnet"
	CodePrivateTrackerRefused    = "private_tracker_refused"
	CodePrivateVerifyUnavailable = "private_verify_unavailable"
	CodePrivateUnavailable       = "private_unavailable"
)

// Reasons of a private_torrent refusal (Error.Reason).
const (
	// ReasonSignIn: the caller is anonymous; signing in may grant access.
	ReasonSignIn = "sign_in"
	// ReasonNoRecord: the caller has no record of adding this torrent with
	// their own tracker.
	ReasonNoRecord = "no_record"
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
	// Reason refines Code: for private_torrent one of sign_in, no_record,
	// token_in_query; for private_tracker_refused the tracker's refusal
	// class; otherwise empty. Branch with errors.As on Code and Reason.
	Reason string
	// RetryAfter is the server-requested pause before retrying; non-zero only
	// on responses that carried a Retry-After header (rate limiting,
	// private_verify_unavailable).
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

// webUIErrorEnvelope is web-ui's error dialect:
// {"error":{"code","message","reason"}}.
type webUIErrorEnvelope struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Reason  string `json:"reason"`
	} `json:"error"`
}

// decodeError normalizes a non-2xx response body into *Error. It recognizes,
// in order: the web-ui envelope, rest-api's {"error","code","reason"} (also
// what RapidAPI passes through), RapidAPI's own {"message":"string"}, and
// anything else (empty 403 from the gateway, HTML from an intermediary) by
// synthesizing the code from the status.
func decodeError(status int, body []byte, hdr http.Header) *Error {
	e := &Error{HTTPStatus: status, RetryAfter: retryAfter(hdr)}

	var env webUIErrorEnvelope
	if err := json.Unmarshal(body, &env); err == nil && env.Error.Code != "" {
		e.Code = env.Error.Code
		e.Message = env.Error.Message
		e.Reason = env.Error.Reason
		return e
	}
	var rest struct {
		Error  string `json:"error"`
		Code   string `json:"code"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(body, &rest); err == nil && (rest.Error != "" || rest.Code != "") {
		e.Message = rest.Error
		e.Reason = rest.Reason
		e.Code = rest.Code
		if e.Code == "" {
			e.Code = codeFromStatus(status)
		}
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
	case http.StatusBadRequest, http.StatusRequestEntityTooLarge, http.StatusUnprocessableEntity:
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
