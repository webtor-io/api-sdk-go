package webtor

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
)

// maxResumes bounds how many times a download re-resolves its export URL and
// resumes with a Range request after a mid-stream failure or URL expiry.
const maxResumes = 5

// DownloadOption configures OpenDownload.
type DownloadOption func(*downloadConfig)

type downloadConfig struct {
	offset int64
}

// WithOffset starts the download at byte n (resume of a partial file).
func WithOffset(n int64) DownloadOption {
	return func(c *downloadConfig) { c.offset = n }
}

// Download is a resumable byte stream of one file. It implements
// io.ReadCloser; on a mid-stream failure (or expiry of the short-lived
// export URL) it transparently re-resolves a fresh URL and resumes with a
// Range request, up to 5 times. The context passed to OpenDownload governs
// the whole stream.
type Download struct {
	// Name is the file's name, Size its total length in bytes.
	Name string
	Size int64

	ctx     context.Context
	hc      *http.Client
	ua      string
	resolve func(context.Context) (string, error)

	body    io.ReadCloser
	read    atomic.Int64 // bytes delivered to the caller
	offset  int64        // starting offset (WithOffset)
	resumes int
}

// OpenDownload opens the content of one file for reading. contentID is a
// ListItem.ID or a file index as a decimal string. The export URL is
// resolved immediately before the GET and re-resolved on resume — the URLs
// are short-lived and must never be persisted.
func (c *Client) OpenDownload(ctx context.Context, resourceID, contentID string, opts ...DownloadOption) (*Download, error) {
	cfg := downloadConfig{}
	for _, o := range opts {
		o(&cfg)
	}
	if cfg.offset < 0 {
		return nil, fmt.Errorf("webtor: negative download offset")
	}
	resolve := func(ctx context.Context) (string, error) {
		resp, err := c.Export(ctx, resourceID, contentID, ExportOptions{
			Types: []ExportType{ExportTypeDownload},
		})
		if err != nil {
			return "", err
		}
		u, ok := resp.DownloadURL()
		if !ok {
			return "", fmt.Errorf("webtor: no download export for content %q", contentID)
		}
		return u, nil
	}

	// The descriptor comes from the same export call the first URL does.
	resp, err := c.Export(ctx, resourceID, contentID, ExportOptions{
		Types: []ExportType{ExportTypeDownload},
	})
	if err != nil {
		return nil, err
	}
	u, ok := resp.DownloadURL()
	if !ok {
		return nil, fmt.Errorf("webtor: no download export for content %q", contentID)
	}
	if resp.Source.Type == ListTypeDirectory {
		return nil, fmt.Errorf("webtor: content %q is a directory — use OpenArchive", contentID)
	}
	if cfg.offset > resp.Source.Size {
		return nil, fmt.Errorf("webtor: offset %d beyond file size %d", cfg.offset, resp.Source.Size)
	}

	d := &Download{
		Name:    resp.Source.Name,
		Size:    resp.Source.Size,
		ctx:     ctx,
		hc:      c.hc,
		ua:      c.ua,
		resolve: resolve,
		offset:  cfg.offset,
	}
	if err := d.open(u); err != nil {
		return nil, err
	}
	return d, nil
}

// open issues the GET for the current position against url.
func (d *Download) open(url string) error {
	req, err := http.NewRequestWithContext(d.ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", d.ua)
	pos := d.offset + d.read.Load()
	if pos > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", pos))
	}
	resp, err := d.hc.Do(req)
	if err != nil {
		return &transportError{err: err}
	}
	switch {
	case resp.StatusCode == http.StatusPartialContent:
		d.body = resp.Body
		return nil
	case resp.StatusCode == http.StatusOK:
		if pos > 0 {
			// The edge ignored Range; skip what we already delivered rather
			// than hand out duplicate bytes.
			if _, err := io.CopyN(io.Discard, resp.Body, pos); err != nil {
				_ = resp.Body.Close()
				return &transportError{err: err}
			}
		}
		d.body = resp.Body
		return nil
	default:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		_ = resp.Body.Close()
		return decodeError(resp.StatusCode, body, resp.Header)
	}
}

// Read implements io.Reader with transparent resume.
func (d *Download) Read(p []byte) (int, error) {
	for {
		n, err := d.body.Read(p)
		d.read.Add(int64(n))
		if n > 0 || err == nil {
			// A short read with a pending error is delivered first; the error
			// resurfaces on the next call.
			return n, nil
		}
		if err == io.EOF {
			if d.remaining() > 0 {
				// Truncated stream (the export URL died mid-flight). Resume.
				if rerr := d.reopen(); rerr != nil {
					return 0, fmt.Errorf("webtor: resuming truncated download: %w", rerr)
				}
				continue
			}
			return 0, io.EOF
		}
		if d.ctx.Err() != nil {
			return 0, d.ctx.Err()
		}
		if rerr := d.reopen(); rerr != nil {
			return 0, fmt.Errorf("webtor: resuming download after %q: %w", err, rerr)
		}
	}
}

func (d *Download) remaining() int64 { return d.Size - d.offset - d.read.Load() }

func (d *Download) reopen() error {
	if d.resumes >= maxResumes {
		return fmt.Errorf("webtor: gave up after %d resumes", d.resumes)
	}
	d.resumes++
	_ = d.body.Close()
	u, err := d.resolve(d.ctx)
	if err != nil {
		return err
	}
	return d.open(u)
}

// BytesRead returns how many bytes have been delivered so far (not counting
// the starting offset). Safe to call concurrently with Read for progress
// reporting.
func (d *Download) BytesRead() int64 { return d.read.Load() }

// Close releases the underlying connection.
func (d *Download) Close() error {
	if d.body == nil {
		return nil
	}
	return d.body.Close()
}

// ArchiveFormat selects the container for directory downloads.
type ArchiveFormat string

const (
	ArchiveFormatZip ArchiveFormat = "zip"
	ArchiveFormatTar ArchiveFormat = "tar"
)

// OpenArchive opens a directory (or a selection of paths inside it) as a
// single archive stream. Unlike OpenDownload it cannot resume: the archive is
// packed on the fly and a re-request is not guaranteed byte-identical.
// contentID addresses the directory ("" or the root ID for the whole
// torrent).
func (c *Client) OpenArchive(ctx context.Context, resourceID, contentID string, format ArchiveFormat, paths []string) (io.ReadCloser, string, error) {
	resp, err := c.Export(ctx, resourceID, contentID, ExportOptions{
		Types:         []ExportType{ExportTypeDownload},
		ArchiveFormat: string(format),
		Paths:         paths,
	})
	if err != nil {
		return nil, "", err
	}
	u, ok := resp.DownloadURL()
	if !ok {
		return nil, "", fmt.Errorf("webtor: no download export for content %q", contentID)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", c.ua)
	hresp, err := c.hc.Do(req)
	if err != nil {
		return nil, "", &transportError{err: err}
	}
	if hresp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(hresp.Body, 1<<20))
		_ = hresp.Body.Close()
		return nil, "", decodeError(hresp.StatusCode, body, hresp.Header)
	}
	name := resp.Source.Name
	if name == "" {
		name = resourceID
	}
	return hresp.Body, name + "." + string(format), nil
}
