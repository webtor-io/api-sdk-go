package webtor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// DeviceAuth is an in-flight device authorization. Show UserCode and
// VerificationURI to the person (or open VerificationURIComplete in a
// browser / render it as a QR code), then call WaitDeviceToken.
type DeviceAuth struct {
	UserCode                string
	VerificationURI         string
	VerificationURIComplete string
	ExpiresAt               time.Time

	deviceCode string
	interval   time.Duration
}

// StartDeviceAuth begins the device authorization flow (RFC 8628 shaped).
// deviceName labels the resulting key on the account's "connected devices"
// list — use something recognizable like "webtor-cli @ hostname". The call
// needs no API key. Web-ui backend only.
func (c *Client) StartDeviceAuth(ctx context.Context, deviceName string) (*DeviceAuth, error) {
	if err := c.require(CapDeviceFlow); err != nil {
		return nil, err
	}
	var body []byte
	if deviceName != "" {
		var err error
		body, err = json.Marshal(map[string]string{"name": deviceName})
		if err != nil {
			return nil, err
		}
	}
	var out DeviceCodeResponse
	err := c.do(ctx, apiRequest{
		method:      http.MethodPost,
		path:        "device/code",
		body:        body,
		contentType: "application/json",
	}, &out)
	if err != nil {
		return nil, err
	}
	interval := time.Duration(out.Interval) * time.Second
	if interval <= 0 {
		interval = 5 * time.Second
	}
	expires := time.Duration(out.ExpiresIn) * time.Second
	if expires <= 0 {
		expires = 10 * time.Minute
	}
	return &DeviceAuth{
		UserCode:                out.UserCode,
		VerificationURI:         out.VerificationURI,
		VerificationURIComplete: out.VerificationURIComplete,
		ExpiresAt:               time.Now().Add(expires),
		deviceCode:              out.DeviceCode,
		interval:                interval,
	}, nil
}

// slowDownStep is the interval increase after a slow_down answer
// (RFC 8628 §3.5). Overridden in tests.
var slowDownStep = 5 * time.Second

// ErrDeviceAuthExpired is returned by WaitDeviceToken when the code expired
// (or was already consumed) before the person confirmed — start over with
// StartDeviceAuth.
var ErrDeviceAuthExpired = errors.New("webtor: device authorization expired, start over")

// WaitDeviceToken polls until the person confirms the device on the website
// and returns the freshly issued API key. The key is delivered exactly once —
// persist it before doing anything else. onTick, when non-nil, is called
// before each poll (drive a spinner with it). Polling paces itself by the
// server-announced interval, backs off on slow_down, and stops on context
// cancellation or code expiry.
func (c *Client) WaitDeviceToken(ctx context.Context, da *DeviceAuth, onTick func()) (string, error) {
	if err := c.require(CapDeviceFlow); err != nil {
		return "", err
	}
	if da == nil || da.deviceCode == "" {
		return "", fmt.Errorf("webtor: DeviceAuth must come from StartDeviceAuth")
	}
	body, err := json.Marshal(map[string]string{"device_code": da.deviceCode})
	if err != nil {
		return "", err
	}
	interval := da.interval
	for {
		if !da.ExpiresAt.IsZero() && time.Now().After(da.ExpiresAt) {
			return "", ErrDeviceAuthExpired
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(interval):
		}
		if onTick != nil {
			onTick()
		}
		var out DeviceTokenResponse
		err := c.do(ctx, apiRequest{
			method:      http.MethodPost,
			path:        "device/token",
			body:        body,
			contentType: "application/json",
			// The poll protocol has its own pacing; a generic retry would
			// fight the slow_down signal.
			noRetry: true,
		}, &out)
		if err == nil {
			if out.Key == "" {
				return "", fmt.Errorf("webtor: device token response carried no key")
			}
			return out.Key, nil
		}
		var ae *Error
		if !errors.As(err, &ae) {
			return "", err
		}
		switch ae.Code {
		case CodeAuthorizationPending:
			// keep polling
		case CodeSlowDown, CodeRateLimited:
			interval += slowDownStep
		case CodeExpiredToken:
			return "", ErrDeviceAuthExpired
		default:
			return "", err
		}
	}
}
