package webtor

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

// restBody is rest-api's error body (RapidAPI proxies it as is); an empty
// reason is left out, as the server's omitempty does.
func restBody(code, reason string) string {
	m := map[string]string{"error": "refused", "code": code}
	if reason != "" {
		m["reason"] = reason
	}
	b, _ := json.Marshal(m)
	return string(b)
}

// webUIBody is web-ui's error envelope.
func webUIBody(code, reason string) string {
	m := map[string]string{"code": code, "message": "refused"}
	if reason != "" {
		m["reason"] = reason
	}
	b, _ := json.Marshal(map[string]any{"error": m})
	return string(b)
}

// The one full table of private-torrent codes in the repo.
func TestPrivateCodesAcrossDialects(t *testing.T) {
	cases := []struct {
		code, reason string
		status       int
	}{
		{CodePrivateTorrent, ReasonSignIn, 403},
		{CodePrivateTorrent, ReasonNoRecord, 403},
		{CodePrivateTorrent, "token_in_query", 403},
		{CodePrivateTorrentFile, "", 403},
		{CodePrivateNeedsAccount, "", 422},
		{CodePrivateNoTracker, "", 422},
		{CodePrivateTooManyTrackers, "", 422},
		{CodePrivateMagnet, "", 422},
		{CodePrivateTrackerRefused, "key", 422},
		{CodePrivateVerifyUnavailable, "", 503},
		{CodePrivateUnavailable, "", 403},
		{CodePrivateUnavailable, "", 422},
	}
	dialects := []struct {
		name string
		body func(code, reason string) string
		mk   func(string) (Backend, error)
	}{
		{"webui", webUIBody, webUIAt("0123456789abcdef")},
		{"restapi", restBody, directAt()},
	}
	for _, d := range dialects {
		for _, tc := range cases {
			t.Run(d.name+"/"+tc.code+"/"+tc.reason+"/"+strconv.Itoa(tc.status), func(t *testing.T) {
				c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(tc.status)
					_, _ = w.Write([]byte(d.body(tc.code, tc.reason)))
				}), d.mk)
				c.retry.BaseWait = 2 * time.Millisecond // a GET on 503 is retried
				_, err := c.Resource(context.Background(), sintel)
				var ae *Error
				if !errors.As(err, &ae) || ae.Code != tc.code || ae.Reason != tc.reason || ae.HTTPStatus != tc.status {
					t.Fatalf("err = %#v", err)
				}
				if IsForbidden(err) {
					t.Error("a private refusal must not read as forbidden")
				}
			})
		}
	}
}

func TestTorrentFilePrivate(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/resource/"+sintel+".torrent" {
			w.WriteHeader(404)
			return
		}
		w.WriteHeader(403)
		_, _ = w.Write([]byte(restBody(CodePrivateTorrentFile, "")))
	})
	c := newTestClient(t, h, directAt())
	b, err := c.TorrentFile(context.Background(), sintel)
	var ae *Error
	if b != nil || !errors.As(err, &ae) || ae.Code != CodePrivateTorrentFile {
		t.Errorf("b = %q, err = %#v", b, err)
	}
}

// The record is deleted mid-download: the resume's re-resolve is refused,
// and the refusal stays typed through Read.
func TestDownloadResumeKeepsPrivateError(t *testing.T) {
	f := &downloadFake{content: []byte(strings.Repeat("0123456789", 1000)), cutAt: 3000,
		expireOldGens: true, refuseExportsAfter: 1}
	c := newDownloadClient(t, f)
	d, err := c.OpenDownload(context.Background(), sintel, "a")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	_, err = io.ReadAll(d)
	var ae *Error
	if !errors.As(err, &ae) || ae.Code != CodePrivateTorrent || ae.Reason != ReasonNoRecord {
		t.Errorf("err = %v", err)
	}
}
