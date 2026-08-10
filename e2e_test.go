package webtor

import (
	"context"
	"io"
	"os"
	"testing"
	"time"
)

// TestE2E runs the whole flow against the real API. Gated:
//
//	WEBTOR_E2E_API_KEY=<key> go test -run TestE2E -v
//
// Uses the Sintel test torrent (well-seeded, permanently cached).
func TestE2E(t *testing.T) {
	key := os.Getenv("WEBTOR_E2E_API_KEY")
	if key == "" {
		t.Skip("WEBTOR_E2E_API_KEY not set")
	}
	backend, err := WebUI(key)
	if err != nil {
		t.Fatal(err)
	}
	c, err := New(backend)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	res, err := c.AddResource(ctx, Magnet(sintel))
	if err != nil {
		t.Fatal(err)
	}
	if res.ID != sintel {
		t.Fatalf("resource id = %q", res.ID)
	}

	var video *ListItem
	for it, err := range c.ListAll(ctx, res.ID, ListOptions{}) {
		if err != nil {
			t.Fatal(err)
		}
		if it.MediaFormat == MediaFormatVideo {
			v := it
			video = &v
		}
	}
	if video == nil {
		t.Fatal("no video file in the Sintel torrent")
	}

	exp, err := c.Export(ctx, res.ID, video.ID, ExportOptions{
		Types: []ExportType{ExportTypeDownload, ExportTypeStream},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := exp.DownloadURL(); !ok {
		t.Fatal("no download export")
	}

	d, err := c.OpenDownload(ctx, res.ID, video.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	buf := make([]byte, 1024)
	if _, err := io.ReadFull(d, buf); err != nil {
		t.Fatalf("reading first KiB: %v", err)
	}
	t.Logf("downloaded first KiB of %s (%d bytes total)", d.Name, d.Size)

	if _, err := c.Profile(ctx); err != nil {
		t.Fatalf("profile: %v", err)
	}
}
