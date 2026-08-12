package webtor

import "time"

// The types in this file mirror the wire format of two upstream packages:
//
//   - github.com/webtor-io/rest-api services (ResourceResponse, ListItem,
//     ListResponse, ExportResponse and friends) — served verbatim by every
//     backend;
//   - github.com/webtor-io/web-ui services/libapi (Library*, Vault*, Profile*,
//     Device*) — served only by the web-ui backend.
//
// They are copies, not imports: importing those modules would drag gin and
// client-go into every consumer's build. The conformance sub-module
// round-trips these types against the originals to catch drift.

// ResourceResponse describes a stored torrent.
type ResourceResponse struct {
	ID        string `json:"id"`
	Name      string `json:"name,omitempty"`
	MagnetURI string `json:"magnet_uri,omitempty"`
	// MultiFile is false for single-file-mode torrents (one file sitting at
	// the torrent root).
	MultiFile bool `json:"multi_file"`
	// File is the single file of a single-file torrent (nil when MultiFile).
	// Lets clients skip List for the common single-file case.
	File *ListItem `json:"file,omitempty"`
	// Size is the torrent's total size in bytes (sum of all files).
	Size int64 `json:"size"`
	// FilesCount is the number of files in the torrent.
	FilesCount int `json:"files_count"`
}

// ListType discriminates entries of a torrent listing.
type ListType string

const (
	ListTypeFile      ListType = "file"
	ListTypeDirectory ListType = "directory"
)

// MediaFormat is the coarse media class of a file as detected by the backend.
type MediaFormat string

const (
	MediaFormatAudio    MediaFormat = "audio"
	MediaFormatVideo    MediaFormat = "video"
	MediaFormatImage    MediaFormat = "image"
	MediaFormatSubtitle MediaFormat = "subtitle"
)

// ListItem is a single entry (file or directory) of a torrent listing.
type ListItem struct {
	ID          string      `json:"id"`
	Name        string      `json:"name,omitempty"`
	Path        string      `json:"path"`
	Type        ListType    `json:"type"`
	Size        int64       `json:"size"`
	MediaFormat MediaFormat `json:"media_format,omitempty"`
	MimeType    string      `json:"mime_type,omitempty"`
	Ext         string      `json:"ext,omitempty"`
	// Index is the file's position in the torrent's natural file order, i.e.
	// the content_id accepted by /resource/<hash>/export/<idx>. Valid only
	// for Type == file items; directory items leave it zero.
	Index int `json:"index"`
}

// ListResponse is the answer to a listing request. The embedded ListItem
// describes the listed node itself (the root or the directory at Path).
type ListResponse struct {
	ListItem
	Items []ListItem `json:"items"`
	Count int        `json:"items_count"`
}

// ExportType names an export flavor in ExportResponse.Exports. Every entry is
// optional: an export that does not apply to the file is silently absent.
type ExportType string

const (
	ExportTypeDownload          ExportType = "download"
	ExportTypeStream            ExportType = "stream"
	ExportTypeTorrentClientStat ExportType = "torrent_client_stat"
	ExportTypeSubtitles         ExportType = "subtitles"
	ExportTypeMediaProbe        ExportType = "media_probe"
)

// ExportSource is one playable source of an ExportTag.
type ExportSource struct {
	Src  string `json:"src"`
	Type string `json:"type"`
}

// ExportTrack is a side-car track (subtitles) of an ExportTag.
type ExportTrack struct {
	Src     string `json:"src"`
	Kind    string `json:"kind"`
	SrcLang string `json:"srclang,omitempty"`
	Label   string `json:"label,omitempty"`
}

// ExportTag is a ready-to-render HTML media tag descriptor.
type ExportTag struct {
	Name    string         `json:"tag,omitempty"`
	Preload string         `json:"preload,omitempty"`
	Sources []ExportSource `json:"sources,omitempty"`
	Tracks  []ExportTrack  `json:"tracks,omitempty"`
	Src     string         `json:"src,omitempty"`
	Alt     string         `json:"alt,omitempty"`
	Poster  string         `json:"poster,omitempty"`
}

// ExportMeta carries flags about how the export will be served.
type ExportMeta struct {
	Transcode      bool `json:"transcode,omitempty"`
	Multibitrate   bool `json:"multibitrate,omitempty"`
	Cache          bool `json:"cache,omitempty"`
	TranscodeCache bool `json:"transcode_cache,omitempty"`
}

// ExportItem is one export of a file: a short-lived, self-authorizing URL plus
// optional presentation metadata. Never persist the URL — resolve a fresh one
// right before use.
type ExportItem struct {
	URL  string      `json:"url,omitempty"`
	Tag  *ExportTag  `json:"html_tag,omitempty"`
	Meta *ExportMeta `json:"meta,omitempty"`
}

// ExportResponse is the answer to an export request.
type ExportResponse struct {
	Source  ListItem                  `json:"source"`
	Exports map[ExportType]ExportItem `json:"exports"`
}

// DownloadURL returns the plain-bytes URL, if the download export is present.
func (r *ExportResponse) DownloadURL() (string, bool) {
	e, ok := r.Exports[ExportTypeDownload]
	return e.URL, ok && e.URL != ""
}

// StreamURL returns the streaming (HLS) URL, if the stream export is present.
func (r *ExportResponse) StreamURL() (string, bool) {
	e, ok := r.Exports[ExportTypeStream]
	return e.URL, ok && e.URL != ""
}

// Cached reports whether the content is already fully cached server-side, so
// a download starts instantly instead of waiting on the swarm. Absence of the
// torrent_client_stat export means the same thing.
func (r *ExportResponse) Cached() bool {
	if e, ok := r.Exports[ExportTypeDownload]; ok && e.Meta != nil && e.Meta.Cache {
		return true
	}
	_, stat := r.Exports[ExportTypeTorrentClientStat]
	return !stat
}

// LibraryItem is a torrent saved to the account's library (web-ui only).
type LibraryItem struct {
	ResourceID string    `json:"resource_id"`
	Name       string    `json:"name"`
	Size       int64     `json:"size"`
	FilesCount int       `json:"files_count"`
	AddedAt    time.Time `json:"added_at"`
}

// LibraryListResponse is one page of the account's library.
type LibraryListResponse struct {
	Items  []LibraryItem `json:"items"`
	Count  int           `json:"items_count"`
	Limit  int           `json:"limit"`
	Offset int           `json:"offset"`
	Type   string        `json:"type"`
	Sort   string        `json:"sort"`
	// Watched echoes the applied watched filter; empty when not filtering.
	Watched string `json:"watched,omitempty"`
}

// VaultPoints is the account's Vault point balance. Total and Available are
// nil on unlimited plans.
type VaultPoints struct {
	Total     *float64 `json:"total"`
	Available *float64 `json:"available"`
	Funded    float64  `json:"funded"`
	Frozen    float64  `json:"frozen"`
	Claimable float64  `json:"claimable"`
}

// VaultContent counts the account's vaulted content by state.
type VaultContent struct {
	Vaulted  int `json:"vaulted"`
	Loading  int `json:"loading"`
	Expiring int `json:"expiring"`
}

// Pledge is a commitment of Vault points to keep a torrent stored.
type Pledge struct {
	PledgeID   string    `json:"pledge_id"`
	ResourceID string    `json:"resource_id"`
	Name       string    `json:"name,omitempty"`
	Amount     float64   `json:"amount"`
	Frozen     bool      `json:"frozen"`
	Funded     bool      `json:"funded"`
	Vaulted    bool      `json:"vaulted"`
	Expired    bool      `json:"expired"`
	RequiredVP float64   `json:"required_vp"`
	FundedVP   float64   `json:"funded_vp"`
	CreatedAt  time.Time `json:"created_at"`
}

// VaultResponse is the account's full Vault state.
type VaultResponse struct {
	Points  VaultPoints  `json:"points"`
	Content VaultContent `json:"content"`
	Pledges []Pledge     `json:"pledges"`
}

// PledgeStatus values for PledgeStatusResponse.Status.
const (
	// PledgeStatusWaiting: the pledge exists but the resource is not funded
	// yet, so no transfer has been asked for.
	PledgeStatusWaiting = "waiting"
	// PledgeStatusQueued: funded and handed to storage, transfer not started.
	PledgeStatusQueued = "queued"
	// PledgeStatusStoring: the transfer is running; Progress applies.
	PledgeStatusStoring = "storing"
	// PledgeStatusFailed: the last transfer attempt failed. Terminal for the
	// attempt, not for the resource — storage retries on its own schedule, so
	// keep polling instead of re-pledging.
	PledgeStatusFailed = "failed"
	// PledgeStatusVaulted: the content is stored. Terminal.
	PledgeStatusVaulted = "vaulted"
	// PledgeStatusExpired: the resource lost its funding. Terminal.
	PledgeStatusExpired = "expired"
)

// PledgeStatusResponse is a pledge plus where its transfer stands. Progress
// and the sizes are nil when there is no transfer to measure (waiting,
// expired) — 0 would read as "started".
type PledgeStatusResponse struct {
	Pledge
	Status     string   `json:"status"`
	Progress   *float64 `json:"progress,omitempty"`
	StoredSize *int64   `json:"stored_size,omitempty"`
	TotalSize  *int64   `json:"total_size,omitempty"`
}

// ProfileTier is the account's subscription tier.
type ProfileTier struct {
	ID   uint32 `json:"id"`
	Name string `json:"name,omitempty"`
}

// ProfileSettings are the account settings exposed over the API.
type ProfileSettings struct {
	ShowAdult bool `json:"show_adult"`
}

// ProfileResponse describes the authenticated account.
type ProfileResponse struct {
	UserID   string          `json:"user_id"`
	Email    string          `json:"email,omitempty"`
	Tier     ProfileTier     `json:"tier"`
	Settings ProfileSettings `json:"settings"`
	Scopes   []string        `json:"scopes"`
}

// DeviceCodeResponse starts a device authorization (RFC 8628).
type DeviceCodeResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

// DeviceTokenResponse delivers the API key. It is delivered exactly once —
// persist it before doing anything else.
type DeviceTokenResponse struct {
	Key string `json:"key"`
}
