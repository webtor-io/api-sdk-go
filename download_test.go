package webtor

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

// downloadFake serves /resource/<id>/export/<cid> with a fresh short-lived
// URL per call, and the file bytes at /dl/<gen>, optionally cutting the
// stream or expiring old generations.
type downloadFake struct {
	content []byte
	// cutAt truncates generation 1's response after cutAt bytes.
	cutAt int
	// expireOldGens makes any generation but the latest answer 403.
	expireOldGens bool
	gen           int
	exports       int
	rangesSeen    []string
}

func (f *downloadFake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case strings.Contains(r.URL.Path, "/export/"):
		f.exports++
		f.gen++
		_ = json.NewEncoder(w).Encode(map[string]any{
			"source": map[string]any{
				"id": "a", "name": "movie.mkv", "path": "/movie.mkv",
				"type": "file", "size": len(f.content),
			},
			"exports": map[string]any{
				"download": map[string]any{"url": "http://" + r.Host + "/dl/" + strconv.Itoa(f.gen)},
			},
		})
	case strings.HasPrefix(r.URL.Path, "/dl/"):
		gen, _ := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/dl/"))
		if f.expireOldGens && gen < f.gen {
			w.WriteHeader(403)
			return
		}
		f.rangesSeen = append(f.rangesSeen, r.Header.Get("Range"))
		body := f.content
		start := 0
		if rng := r.Header.Get("Range"); rng != "" {
			_, _ = fmt.Sscanf(rng, "bytes=%d-", &start)
			body = body[start:]
			w.Header().Set("Content-Range",
				fmt.Sprintf("bytes %d-%d/%d", start, len(f.content)-1, len(f.content)))
			w.WriteHeader(206)
		}
		if gen == 1 && f.cutAt > 0 && f.cutAt < len(body) {
			body = body[:f.cutAt]
			w.Header().Set("Content-Length", strconv.Itoa(len(f.content)-start))
		}
		_, _ = w.Write(body)
		if fl, ok := w.(http.Flusher); ok {
			fl.Flush()
		}
	default:
		w.WriteHeader(404)
	}
}

func newDownloadClient(t *testing.T, f *downloadFake) *Client {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	b, err := Direct(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	c, err := New(b)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestDownloadPlain(t *testing.T) {
	f := &downloadFake{content: []byte(strings.Repeat("abcdefgh", 1024))}
	c := newDownloadClient(t, f)
	d, err := c.OpenDownload(context.Background(), sintel, "a")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	got, err := io.ReadAll(d)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(f.content) {
		t.Errorf("read %d bytes, want %d", len(got), len(f.content))
	}
	if d.Name != "movie.mkv" || d.Size != int64(len(f.content)) {
		t.Errorf("Name=%q Size=%d", d.Name, d.Size)
	}
	if d.BytesRead() != int64(len(f.content)) {
		t.Errorf("BytesRead = %d", d.BytesRead())
	}
}

func TestDownloadResumesTruncatedStreamOnFreshURL(t *testing.T) {
	f := &downloadFake{
		content:       []byte(strings.Repeat("0123456789", 1000)),
		cutAt:         3000,
		expireOldGens: true, // the resume must re-resolve, the old URL is dead
	}
	c := newDownloadClient(t, f)
	d, err := c.OpenDownload(context.Background(), sintel, "a")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	got, err := io.ReadAll(d)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(f.content) {
		t.Fatalf("resumed read = %d bytes, want %d", len(got), len(f.content))
	}
	if f.exports != 2 {
		// one export for descriptor+first URL, one re-resolve for the resume
		t.Errorf("exports = %d, want 2", f.exports)
	}
	found := false
	for _, rng := range f.rangesSeen {
		if rng == "bytes=3000-" {
			found = true
		}
	}
	if !found {
		t.Errorf("no resume Range at the cut point; ranges = %v", f.rangesSeen)
	}
}

func TestDownloadWithOffset(t *testing.T) {
	f := &downloadFake{content: []byte("0123456789")}
	c := newDownloadClient(t, f)
	d, err := c.OpenDownload(context.Background(), sintel, "a", WithOffset(4))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	got, err := io.ReadAll(d)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "456789" {
		t.Errorf("got %q", got)
	}
	if len(f.rangesSeen) == 0 || f.rangesSeen[0] != "bytes=4-" {
		t.Errorf("ranges = %v", f.rangesSeen)
	}
}

func TestDownloadOffsetBeyondSize(t *testing.T) {
	f := &downloadFake{content: []byte("0123")}
	c := newDownloadClient(t, f)
	if _, err := c.OpenDownload(context.Background(), sintel, "a", WithOffset(99)); err == nil {
		t.Error("offset beyond size accepted")
	}
}

func TestOpenArchive(t *testing.T) {
	var gotQuery url.Values
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/export/") {
			gotQuery = r.URL.Query()
			_ = json.NewEncoder(w).Encode(map[string]any{
				"source": map[string]any{"id": "root", "name": "Season1", "path": "/", "type": "directory", "size": 10},
				"exports": map[string]any{
					"download": map[string]any{"url": "http://" + r.Host + "/arch"},
				},
			})
			return
		}
		_, _ = w.Write([]byte("TARBYTES"))
	})
	srv := httptest.NewServer(h)
	defer srv.Close()
	b, _ := Direct(srv.URL)
	c, _ := New(b)
	rc, name, err := c.OpenArchive(context.Background(), sintel, "root", ArchiveFormatTar, []string{"/e1.mkv", "/e2.mkv"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rc.Close() }()
	got, _ := io.ReadAll(rc)
	if string(got) != "TARBYTES" || name != "Season1.tar" {
		t.Errorf("got %q name %q", got, name)
	}
	if gotQuery.Get("archive-format") != "tar" {
		t.Errorf("archive-format = %q", gotQuery.Get("archive-format"))
	}
	if paths := gotQuery["paths"]; len(paths) != 2 {
		t.Errorf("paths = %v", paths)
	}
}
