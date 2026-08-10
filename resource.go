package webtor

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// MaxTorrentSize is the largest .torrent body the API accepts (web-ui
// enforces it server-side; the SDK enforces it client-side for every
// backend so oversized uploads fail fast and identically).
const MaxTorrentSize = 8 << 20

// addResourceTimeout bounds AddResource: a cold magnet blocks server-side for
// up to ~3 minutes while the metadata is fetched from the swarm.
const addResourceTimeout = 4 * time.Minute

var infohashRe = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)

// ResourceSource is the payload of AddResource: a magnet link or the raw
// bytes of a .torrent file. Construct with Magnet, TorrentBytes or
// TorrentReader.
type ResourceSource struct {
	data []byte
	err  error
}

// Magnet turns a magnet URI — or a bare 40-hex infohash — into a
// ResourceSource.
func Magnet(uri string) ResourceSource {
	uri = strings.TrimSpace(uri)
	if infohashRe.MatchString(uri) {
		uri = "magnet:?xt=urn:btih:" + strings.ToLower(uri)
	}
	if !strings.HasPrefix(uri, "magnet:") {
		return ResourceSource{err: fmt.Errorf("webtor: %q is not a magnet URI or infohash", uri)}
	}
	return ResourceSource{data: []byte(uri)}
}

// TorrentBytes wraps the raw bytes of a .torrent file.
func TorrentBytes(b []byte) ResourceSource {
	if len(b) == 0 {
		return ResourceSource{err: fmt.Errorf("webtor: empty torrent body")}
	}
	if len(b) > MaxTorrentSize {
		return ResourceSource{err: fmt.Errorf("webtor: torrent body %d bytes exceeds the %d byte limit", len(b), MaxTorrentSize)}
	}
	return ResourceSource{data: b}
}

// TorrentReader reads a .torrent from r (e.g. stdin), enforcing the size
// limit while reading.
func TorrentReader(r io.Reader) ResourceSource {
	b, err := io.ReadAll(io.LimitReader(r, MaxTorrentSize+1))
	if err != nil {
		return ResourceSource{err: fmt.Errorf("webtor: reading torrent: %w", err)}
	}
	return TorrentBytes(b)
}

// AddResource stores a torrent (magnet or .torrent bytes) and returns its
// descriptor. Adding is content-addressed and idempotent: re-adding the same
// torrent returns the same resource. A cold magnet blocks until the metadata
// is resolved from the swarm — up to ~3 minutes; the call is bounded at 4
// minutes and is deliberately not retried on timeout (the server-side add
// keeps running; re-calling AddResource with the same magnet is the way to
// poll).
func (c *Client) AddResource(ctx context.Context, src ResourceSource) (*ResourceResponse, error) {
	if src.err != nil {
		return nil, src.err
	}
	if len(src.data) == 0 {
		return nil, fmt.Errorf("webtor: empty resource source")
	}
	var out ResourceResponse
	// The trailing slash is load-bearing: plain rest-api registers only
	// POST /resource/ and would answer the bare path with a 307 that not
	// every HTTP client re-POSTs.
	err := c.do(ctx, apiRequest{
		method:      http.MethodPost,
		path:        "resource/",
		body:        src.data,
		contentType: "application/octet-stream",
		timeout:     addResourceTimeout,
	}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// normalizeResourceID lowercases infohashes: the backends are strict about
// lowercase and an uppercase hash would split identity between endpoints.
func normalizeResourceID(id string) string {
	if infohashRe.MatchString(id) {
		return strings.ToLower(id)
	}
	return id
}

// Resource returns the descriptor of a stored torrent. Single-file torrents
// carry File, so no List round-trip is needed to address their content.
func (c *Client) Resource(ctx context.Context, resourceID string) (*ResourceResponse, error) {
	var out ResourceResponse
	err := c.do(ctx, apiRequest{
		method: http.MethodGet,
		path:   "resource/" + url.PathEscape(normalizeResourceID(resourceID)),
	}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// TorrentFile returns the raw .torrent bytes of a stored resource.
func (c *Client) TorrentFile(ctx context.Context, resourceID string) ([]byte, error) {
	return c.doRaw(ctx, apiRequest{
		method: http.MethodGet,
		path:   "resource/" + url.PathEscape(normalizeResourceID(resourceID)) + ".torrent",
	})
}

// ListOutput selects the shape of a listing.
type ListOutput string

const (
	// ListOutputFlat is the flat file list (directories omitted).
	ListOutputFlat ListOutput = "list"
	// ListOutputTree lists one directory level at Path.
	ListOutputTree ListOutput = "tree"
)

// ListSort orders a listing. The zero value keeps the torrent's natural file
// order.
type ListSort string

const (
	ListSortName ListSort = "name"
	ListSortSize ListSort = "size"
)

// ListOptions parameterize List.
type ListOptions struct {
	// Path scopes a tree listing to a directory ("" = root).
	Path string
	// Limit is the page size. 0 means DefaultListLimit. The SDK always sends
	// it explicitly: the backends' server-side defaults disagree (10 on
	// web-ui vs 1000 on rest-api).
	Limit  int
	Offset int
	// Output defaults to ListOutputFlat.
	Output ListOutput
	// Sort defaults to the torrent's natural file order.
	Sort ListSort
}

// DefaultListLimit is the page size used when ListOptions.Limit is zero.
const DefaultListLimit = 100

// List returns one page of a resource's content listing.
func (c *Client) List(ctx context.Context, resourceID string, o ListOptions) (*ListResponse, error) {
	q := url.Values{}
	limit := o.Limit
	if limit <= 0 {
		limit = DefaultListLimit
	}
	q.Set("limit", strconv.Itoa(limit))
	q.Set("offset", strconv.Itoa(max(o.Offset, 0)))
	if o.Path != "" {
		q.Set("path", o.Path)
	}
	if o.Output != "" {
		q.Set("output", string(o.Output))
	}
	if o.Sort != "" {
		q.Set("sort", string(o.Sort))
	}
	var out ListResponse
	err := c.do(ctx, apiRequest{
		method: http.MethodGet,
		path:   "resource/" + url.PathEscape(normalizeResourceID(resourceID)) + "/list",
		query:  q,
	}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// ListAll iterates every item of a listing, paging transparently. Iteration
// stops at the first error, yielded with a zero ListItem.
func (c *Client) ListAll(ctx context.Context, resourceID string, o ListOptions) func(yield func(ListItem, error) bool) {
	return func(yield func(ListItem, error) bool) {
		offset := max(o.Offset, 0)
		for {
			page := o
			page.Offset = offset
			resp, err := c.List(ctx, resourceID, page)
			if err != nil {
				yield(ListItem{}, err)
				return
			}
			for _, it := range resp.Items {
				if !yield(it, nil) {
					return
				}
			}
			offset += len(resp.Items)
			if len(resp.Items) == 0 || offset >= resp.Count {
				return
			}
		}
	}
}

// ExportOptions parameterize Export.
type ExportOptions struct {
	// Types filters which exports to produce; empty means all. The SDK uses
	// the `types` parameter, which every backend understands (web-ui's
	// `output` parameter is deliberately not exposed — it does not exist on
	// the other backends).
	Types []ExportType
	// ArchiveFormat picks the archive container for directory downloads:
	// "zip" (default server-side) or "tar".
	ArchiveFormat string
	// Paths restricts a directory archive to the given paths.
	Paths []string
	// IMDBID improves subtitle lookup for the subtitles export.
	IMDBID string
}

// Export resolves the short-lived, self-authorizing URLs for one file (or
// directory) of a resource. contentID is a ListItem.ID, a file's index in
// the torrent's natural order (ListItem.Index) as a decimal string, or a
// directory's ID for archive downloads. Every entry of the result map is
// optional — an export that does not apply to the file is silently absent.
// The URLs expire: resolve immediately before use and never persist them.
func (c *Client) Export(ctx context.Context, resourceID, contentID string, o ExportOptions) (*ExportResponse, error) {
	q := url.Values{}
	if len(o.Types) > 0 {
		ts := make([]string, len(o.Types))
		for i, t := range o.Types {
			ts[i] = string(t)
		}
		q.Set("types", strings.Join(ts, ","))
	}
	if o.ArchiveFormat != "" {
		q.Set("archive-format", o.ArchiveFormat)
	}
	for _, p := range o.Paths {
		q.Add("paths", p)
	}
	if o.IMDBID != "" {
		q.Set("imdb-id", o.IMDBID)
	}
	var out ExportResponse
	err := c.do(ctx, apiRequest{
		method: http.MethodGet,
		path: "resource/" + url.PathEscape(normalizeResourceID(resourceID)) +
			"/export/" + url.PathEscape(contentID),
		query: q,
	}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}
