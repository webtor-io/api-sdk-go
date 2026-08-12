package webtor

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
)

// LibraryType filters a library listing by content kind.
type LibraryType string

const (
	LibraryTypeAll    LibraryType = "all"
	LibraryTypeMovies LibraryType = "movies"
	LibraryTypeSeries LibraryType = "series"
)

// LibrarySort orders a library listing.
type LibrarySort string

const (
	LibrarySortRecent LibrarySort = "recent"
	LibrarySortName   LibrarySort = "name"
	// LibrarySortYear orders by release year; the API accepts it only for
	// the movies and series sections.
	LibrarySortYear LibrarySort = "year"
	// LibrarySortRating orders by rating; movies and series sections only.
	LibrarySortRating LibrarySort = "rating"
)

// LibraryWatched filters a library listing by watched state; the API accepts
// the non-all values only for the movies and series sections.
type LibraryWatched string

const (
	LibraryWatchedAll       LibraryWatched = "all"
	LibraryWatchedWatched   LibraryWatched = "watched"
	LibraryWatchedUnwatched LibraryWatched = "unwatched"
)

// LibraryListOptions parameterize LibraryList.
type LibraryListOptions struct {
	Type LibraryType // default all
	Sort LibrarySort // default recent
	// Watched filters by watched state (movies/series sections only).
	Watched LibraryWatched // default all
	// Limit is the page size; 0 keeps the server default (100, max 1000).
	Limit  int
	Offset int
}

// LibraryList returns one page of the account's library. Web-ui backend only.
func (c *Client) LibraryList(ctx context.Context, o LibraryListOptions) (*LibraryListResponse, error) {
	if err := c.require(CapLibrary); err != nil {
		return nil, err
	}
	q := url.Values{}
	if o.Type != "" {
		q.Set("type", string(o.Type))
	}
	if o.Sort != "" {
		q.Set("sort", string(o.Sort))
	}
	if o.Watched != "" && o.Watched != LibraryWatchedAll {
		q.Set("watched", string(o.Watched))
	}
	if o.Limit > 0 {
		q.Set("limit", strconv.Itoa(o.Limit))
	}
	if o.Offset > 0 {
		q.Set("offset", strconv.Itoa(o.Offset))
	}
	var out LibraryListResponse
	if err := c.do(ctx, apiRequest{method: http.MethodGet, path: "library", query: q}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// LibraryGet returns the library entry for a resource; a not_found error
// means the resource is not in the library (the membership check).
func (c *Client) LibraryGet(ctx context.Context, resourceID string) (*LibraryItem, error) {
	if err := c.require(CapLibrary); err != nil {
		return nil, err
	}
	var out LibraryItem
	err := c.do(ctx, apiRequest{
		method: http.MethodGet,
		path:   "library/" + url.PathEscape(normalizeResourceID(resourceID)),
	}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// LibraryAdd adds a stored resource to the account's library. Idempotent:
// adding an already-present resource succeeds and returns the existing entry.
func (c *Client) LibraryAdd(ctx context.Context, resourceID string) (*LibraryItem, error) {
	if err := c.require(CapLibrary); err != nil {
		return nil, err
	}
	body, err := json.Marshal(map[string]string{"resource_id": normalizeResourceID(resourceID)})
	if err != nil {
		return nil, err
	}
	var out LibraryItem
	err = c.do(ctx, apiRequest{
		method:      http.MethodPost,
		path:        "library",
		body:        body,
		contentType: "application/json",
	}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// LibraryRename renames a library entry (the name shown in the library UI,
// WebDAV and S3 views alike).
func (c *Client) LibraryRename(ctx context.Context, resourceID, name string) (*LibraryItem, error) {
	if err := c.require(CapLibrary); err != nil {
		return nil, err
	}
	body, err := json.Marshal(map[string]string{"name": name})
	if err != nil {
		return nil, err
	}
	var out LibraryItem
	err = c.do(ctx, apiRequest{
		method:      http.MethodPatch,
		path:        "library/" + url.PathEscape(normalizeResourceID(resourceID)),
		body:        body,
		contentType: "application/json",
	}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// LibraryRemove removes a resource from the library. Not idempotent: removing
// an absent entry returns a not_found error.
func (c *Client) LibraryRemove(ctx context.Context, resourceID string) error {
	if err := c.require(CapLibrary); err != nil {
		return err
	}
	return c.do(ctx, apiRequest{
		method: http.MethodDelete,
		path:   "library/" + url.PathEscape(normalizeResourceID(resourceID)),
	}, nil)
}
