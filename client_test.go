package webtor

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const sintel = "08ada5a7a6183aae1e09d831df6748d566095a10"

func newTestClient(t *testing.T, h http.Handler, mk func(base string) (Backend, error)) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	b, err := mk(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	c, err := New(b)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func webUIAt(key string) func(string) (Backend, error) {
	return func(base string) (Backend, error) { return WebUI(key, WithWebUIBaseURL(base)) }
}

func directAt(opts ...DirectOption) func(string) (Backend, error) {
	return func(base string) (Backend, error) { return Direct(base, opts...) }
}

// --- error decoding across the three dialects ---

func TestDecodeErrorDialects(t *testing.T) {
	hdr := http.Header{}
	for _, tc := range []struct {
		name     string
		status   int
		body     string
		wantCode string
		wantMsg  string
	}{
		{"webui envelope", 404, `{"error":{"code":"not_found","message":"no such path"}}`, CodeNotFound, "no such path"},
		{"webui envelope keeps code over status", 400, `{"error":{"code":"authorization_pending","message":"waiting"}}`, CodeAuthorizationPending, "waiting"},
		{"restapi string", 404, `{"error":"resource not found"}`, CodeNotFound, "resource not found"},
		{"restapi string 400", 400, `{"error":"failed to parse torrent"}`, CodeBadRequest, "failed to parse torrent"},
		{"rapidapi message", 429, `{"message":"Too many requests"}`, CodeRateLimited, "Too many requests"},
		{"empty 403 gateway", 403, ``, CodeForbidden, ""},
		{"html intermediary", 502, `<html>bad gateway</html>`, CodeUpstream, ""},
		{"408 timeout", 408, `{"error":"timeout"}`, CodeUpstreamTimeout, "timeout"},
		{"413 oversized", 413, ``, CodeBadRequest, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := decodeError(tc.status, []byte(tc.body), hdr)
			if e.Code != tc.wantCode || e.Message != tc.wantMsg || e.HTTPStatus != tc.status {
				t.Errorf("got code=%q msg=%q status=%d, want code=%q msg=%q status=%d",
					e.Code, e.Message, e.HTTPStatus, tc.wantCode, tc.wantMsg, tc.status)
			}
		})
	}
}

func TestRetryAfterParsed(t *testing.T) {
	hdr := http.Header{}
	hdr.Set("Retry-After", "7")
	e := decodeError(429, []byte(`{"error":{"code":"rate_limited","message":"slow down"}}`), hdr)
	if e.RetryAfter != 7*time.Second {
		t.Errorf("RetryAfter = %v, want 7s", e.RetryAfter)
	}
}

// --- auth headers per backend ---

func TestBackendAuthHeaders(t *testing.T) {
	var got http.Header
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		_, _ = w.Write([]byte(`{"id":"` + sintel + `","multi_file":true,"size":1,"files_count":1}`))
	})

	t.Run("webui bearer", func(t *testing.T) {
		c := newTestClient(t, h, webUIAt("11111111-2222-3333-4444-555555555555"))
		if _, err := c.Resource(context.Background(), sintel); err != nil {
			t.Fatal(err)
		}
		if a := got.Get("Authorization"); a != "Bearer 11111111-2222-3333-4444-555555555555" {
			t.Errorf("Authorization = %q", a)
		}
	})

	t.Run("direct pass-through creds", func(t *testing.T) {
		c := newTestClient(t, h, directAt(WithDirectAPIKey("k1"), WithDirectToken("t1")))
		if _, err := c.Resource(context.Background(), sintel); err != nil {
			t.Fatal(err)
		}
		if got.Get("X-Api-Key") != "k1" || got.Get("X-Token") != "t1" {
			t.Errorf("X-Api-Key=%q X-Token=%q", got.Get("X-Api-Key"), got.Get("X-Token"))
		}
	})

	t.Run("rapidapi headers", func(t *testing.T) {
		b, err := RapidAPI("rk")
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest("GET", "https://webtor.p.rapidapi.com/resource/x", nil)
		b.Authorize(req)
		if req.Header.Get("X-RapidAPI-Key") != "rk" || req.Header.Get("X-RapidAPI-Host") != "webtor.p.rapidapi.com" {
			t.Errorf("headers = %v", req.Header)
		}
	})
}

// --- resource surface invariants ---

func TestAddResourceUsesTrailingSlash(t *testing.T) {
	var gotPath, gotBody string
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		b := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(b)
		gotBody = string(b)
		_, _ = w.Write([]byte(`{"id":"` + sintel + `","multi_file":false,"size":1,"files_count":1}`))
	})
	c := newTestClient(t, h, directAt())
	if _, err := c.AddResource(context.Background(), Magnet(sintel)); err != nil {
		t.Fatal(err)
	}
	// Trailing slash is load-bearing: bare /resource on plain rest-api
	// answers 307 and not every client re-POSTs the body.
	if gotPath != "/resource/" {
		t.Errorf("path = %q, want /resource/", gotPath)
	}
	if gotBody != "magnet:?xt=urn:btih:"+sintel {
		t.Errorf("body = %q", gotBody)
	}
}

func TestMagnetNormalizesInfohash(t *testing.T) {
	src := Magnet(strings.ToUpper(sintel))
	if src.err != nil {
		t.Fatal(src.err)
	}
	if string(src.data) != "magnet:?xt=urn:btih:"+sintel {
		t.Errorf("data = %q", src.data)
	}
	if src := Magnet("http://example.com/x.torrent"); src.err == nil {
		t.Error("URL accepted as magnet")
	}
}

func TestTorrentBytesEnforcesLimit(t *testing.T) {
	if src := TorrentBytes(make([]byte, MaxTorrentSize+1)); src.err == nil {
		t.Error("oversized body accepted")
	}
	if src := TorrentReader(strings.NewReader(strings.Repeat("x", MaxTorrentSize+1))); src.err == nil {
		t.Error("oversized reader accepted")
	}
	if src := TorrentBytes(nil); src.err == nil {
		t.Error("empty body accepted")
	}
}

func TestListAlwaysSendsLimit(t *testing.T) {
	var gotQuery map[string][]string
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		_, _ = w.Write([]byte(`{"id":"x","path":"/","type":"directory","size":1,"items":[],"items_count":0}`))
	})
	c := newTestClient(t, h, webUIAt("k"))
	if _, err := c.List(context.Background(), sintel, ListOptions{}); err != nil {
		t.Fatal(err)
	}
	// The server-side defaults disagree (10 on web-ui vs 1000 on rest-api),
	// so an implicit limit would silently change page size across backends.
	if got := gotQuery["limit"]; len(got) != 1 || got[0] != "100" {
		t.Errorf("limit = %v, want [100]", got)
	}
	if got := gotQuery["offset"]; len(got) != 1 || got[0] != "0" {
		t.Errorf("offset = %v, want [0]", got)
	}
}

func TestListAllPaginates(t *testing.T) {
	pages := []string{
		`{"id":"x","path":"/","type":"directory","size":3,"items":[
			{"id":"a","path":"/a","type":"file","size":1,"index":0},
			{"id":"b","path":"/b","type":"file","size":1,"index":1}],"items_count":3}`,
		`{"id":"x","path":"/","type":"directory","size":3,"items":[
			{"id":"c","path":"/c","type":"file","size":1,"index":2}],"items_count":3}`,
	}
	var offsets []string
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		offsets = append(offsets, r.URL.Query().Get("offset"))
		page := pages[0]
		if r.URL.Query().Get("offset") != "0" {
			page = pages[1]
		}
		_, _ = w.Write([]byte(page))
	})
	c := newTestClient(t, h, webUIAt("k"))
	var ids []string
	for it, err := range c.ListAll(context.Background(), sintel, ListOptions{Limit: 2}) {
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, it.ID)
	}
	if strings.Join(ids, ",") != "a,b,c" {
		t.Errorf("ids = %v", ids)
	}
	if strings.Join(offsets, ",") != "0,2" {
		t.Errorf("offsets = %v", offsets)
	}
}

func TestExportUsesTypesParam(t *testing.T) {
	var gotQuery map[string][]string
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		_, _ = w.Write([]byte(`{"source":{"id":"a","path":"/a","type":"file","size":1},
			"exports":{"download":{"url":"http://dl/x","meta":{"cache":true}}}}`))
	})
	c := newTestClient(t, h, webUIAt("k"))
	resp, err := c.Export(context.Background(), sintel, "a", ExportOptions{
		Types: []ExportType{ExportTypeDownload, ExportTypeStream},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := gotQuery["types"]; len(got) != 1 || got[0] != "download,stream" {
		t.Errorf("types = %v", got)
	}
	if _, ok := gotQuery["output"]; ok {
		t.Error("web-ui-only output param must never be sent")
	}
	if u, ok := resp.DownloadURL(); !ok || u != "http://dl/x" {
		t.Errorf("DownloadURL = %q, %v", u, ok)
	}
	if _, ok := resp.StreamURL(); ok {
		t.Error("StreamURL present for absent export")
	}
	if !resp.Cached() {
		t.Error("Cached() = false with meta.cache=true and no stat export")
	}
}

// --- capability gating ---

func TestCapabilityGating(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("capability-gated call must not reach the network")
	})
	c := newTestClient(t, h, directAt())
	_, err := c.LibraryList(context.Background(), LibraryListOptions{})
	var ce *CapabilityError
	if !errors.As(err, &ce) {
		t.Fatalf("err = %v, want CapabilityError", err)
	}
	if ce.Backend != KindDirect || ce.Capability != CapLibrary {
		t.Errorf("CapabilityError = %+v", ce)
	}
	if !strings.Contains(ce.Error(), "library") || !strings.Contains(ce.Error(), "direct") {
		t.Errorf("message = %q", ce.Error())
	}
	if _, err := c.Vault(context.Background()); !errors.As(err, &ce) {
		t.Errorf("Vault err = %v", err)
	}
	if _, err := c.Profile(context.Background()); !errors.As(err, &ce) {
		t.Errorf("Profile err = %v", err)
	}
}

// --- retry behavior ---

func TestRetryHonorsRetryAfter(t *testing.T) {
	var calls int
	var stamps []time.Time
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		stamps = append(stamps, time.Now())
		if calls == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(429)
			_, _ = w.Write([]byte(`{"error":{"code":"rate_limited","message":"slow down"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"` + sintel + `","multi_file":true,"size":1,"files_count":1}`))
	})
	c := newTestClient(t, h, webUIAt("k"))
	if _, err := c.Resource(context.Background(), sintel); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
	if wait := stamps[1].Sub(stamps[0]); wait < time.Second {
		t.Errorf("waited %v before retry, want >= 1s (Retry-After)", wait)
	}
}

func TestPostNotRetriedOnUpstreamTimeout(t *testing.T) {
	var calls int
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(408)
		_, _ = w.Write([]byte(`{"error":"timeout"}`))
	})
	c := newTestClient(t, h, directAt())
	_, err := c.AddResource(context.Background(), Magnet(sintel))
	if err == nil {
		t.Fatal("want error")
	}
	// The server-side add keeps running after a timeout; an automatic re-POST
	// would just stack identical resolves.
	if calls != 1 {
		t.Errorf("calls = %d, want 1 (no retry)", calls)
	}
	var ae *Error
	if !errors.As(err, &ae) || ae.Code != CodeUpstreamTimeout {
		t.Errorf("err = %v", err)
	}
}

func TestGetRetriedOnUpstreamError(t *testing.T) {
	var calls int
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(502)
			return
		}
		_, _ = w.Write([]byte(`{"id":"` + sintel + `","multi_file":true,"size":1,"files_count":1}`))
	})
	c := newTestClient(t, h, func(base string) (Backend, error) { return Direct(base) })
	c.retry.BaseWait = 2 * time.Millisecond
	if _, err := c.Resource(context.Background(), sintel); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Errorf("calls = %d, want 2", calls)
	}
}

func TestErrorPredicates(t *testing.T) {
	err := error(&Error{HTTPStatus: 402, Code: CodePaymentRequired, Message: "paid plans only"})
	if !IsPaymentRequired(err) || IsNotFound(err) {
		t.Error("predicates misclassify")
	}
}
