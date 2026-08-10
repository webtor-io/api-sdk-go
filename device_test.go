package webtor

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"
)

// deviceFake scripts the /device endpoints: the token poll answers from the
// answers queue, one entry per poll.
type deviceFake struct {
	answers []string // "pending", "slow_down", "expired", "key"
	polls   int
	stamps  []time.Time
}

func (f *deviceFake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/device/code":
		_ = json.NewEncoder(w).Encode(DeviceCodeResponse{
			DeviceCode:              "6c0b8bad-4b41-4bcb-9d10-4c0a0a8e1e3f",
			UserCode:                "F7KQ-29XD",
			VerificationURI:         "https://webtor.io/device",
			VerificationURIComplete: "https://webtor.io/device?code=F7KQ-29XD",
			ExpiresIn:               600,
			Interval:                0, // exercised: SDK must not spin on a 0 interval
		})
	case "/device/token":
		f.stamps = append(f.stamps, time.Now())
		a := "pending"
		if f.polls < len(f.answers) {
			a = f.answers[f.polls]
		}
		f.polls++
		switch a {
		case "pending":
			w.WriteHeader(400)
			_, _ = w.Write([]byte(`{"error":{"code":"authorization_pending","message":"waiting"}}`))
		case "slow_down":
			w.WriteHeader(400)
			_, _ = w.Write([]byte(`{"error":{"code":"slow_down","message":"poll slower"}}`))
		case "expired":
			w.WriteHeader(400)
			_, _ = w.Write([]byte(`{"error":{"code":"expired_token","message":"gone"}}`))
		case "key":
			_, _ = w.Write([]byte(`{"key":"99999999-8888-7777-6666-555555555555"}`))
		}
	default:
		w.WriteHeader(404)
	}
}

// shortPoll rewrites the announced interval so tests run in milliseconds.
func shortPoll(t *testing.T, c *Client, name string) *DeviceAuth {
	t.Helper()
	da, err := c.StartDeviceAuth(context.Background(), name)
	if err != nil {
		t.Fatal(err)
	}
	if da.UserCode != "F7KQ-29XD" || da.VerificationURIComplete == "" {
		t.Fatalf("DeviceAuth = %+v", da)
	}
	da.interval = 5 * time.Millisecond
	return da
}

func TestDeviceFlowPendingThenKey(t *testing.T) {
	f := &deviceFake{answers: []string{"pending", "pending", "key"}}
	c := newTestClient(t, f, webUIAt(""))
	da := shortPoll(t, c, "webtor-cli @ test")
	var ticks int
	key, err := c.WaitDeviceToken(context.Background(), da, func() { ticks++ })
	if err != nil {
		t.Fatal(err)
	}
	if key != "99999999-8888-7777-6666-555555555555" {
		t.Errorf("key = %q", key)
	}
	if ticks != 3 || f.polls != 3 {
		t.Errorf("ticks = %d, polls = %d, want 3", ticks, f.polls)
	}
}

func TestDeviceFlowSlowDownBacksOff(t *testing.T) {
	prev := slowDownStep
	slowDownStep = 50 * time.Millisecond
	t.Cleanup(func() { slowDownStep = prev })

	f := &deviceFake{answers: []string{"slow_down", "key"}}
	c := newTestClient(t, f, webUIAt(""))
	da := shortPoll(t, c, "")
	if _, err := c.WaitDeviceToken(context.Background(), da, nil); err != nil {
		t.Fatal(err)
	}
	// After slow_down the next poll must wait the increased interval
	// (5ms + slowDownStep per RFC 8628 §3.5).
	if gap := f.stamps[1].Sub(f.stamps[0]); gap < 50*time.Millisecond {
		t.Errorf("gap after slow_down = %v, want >= 50ms", gap)
	}
}

func TestDeviceFlowExpired(t *testing.T) {
	f := &deviceFake{answers: []string{"expired"}}
	c := newTestClient(t, f, webUIAt(""))
	da := shortPoll(t, c, "")
	_, err := c.WaitDeviceToken(context.Background(), da, nil)
	if !errors.Is(err, ErrDeviceAuthExpired) {
		t.Errorf("err = %v, want ErrDeviceAuthExpired", err)
	}
}

func TestDeviceFlowDeadline(t *testing.T) {
	f := &deviceFake{} // pending forever
	c := newTestClient(t, f, webUIAt(""))
	da := shortPoll(t, c, "")
	da.ExpiresAt = time.Now().Add(20 * time.Millisecond)
	_, err := c.WaitDeviceToken(context.Background(), da, nil)
	if !errors.Is(err, ErrDeviceAuthExpired) {
		t.Errorf("err = %v, want ErrDeviceAuthExpired", err)
	}
}

func TestDeviceFlowContextCancel(t *testing.T) {
	f := &deviceFake{}
	c := newTestClient(t, f, webUIAt(""))
	da := shortPoll(t, c, "")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err := c.WaitDeviceToken(ctx, da, nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want context.DeadlineExceeded", err)
	}
}

func TestDeviceFlowNotOnDirect(t *testing.T) {
	c := newTestClient(t, http.NotFoundHandler(), directAt())
	_, err := c.StartDeviceAuth(context.Background(), "x")
	var ce *CapabilityError
	if !errors.As(err, &ce) {
		t.Errorf("err = %v, want CapabilityError", err)
	}
}
