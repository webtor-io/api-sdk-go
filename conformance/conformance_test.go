// Package conformance pins the SDK's hand-copied wire types to the upstream
// originals. It is a separate Go module on purpose: the upstream packages
// drag gin and client-go with them, and those must never enter the SDK's own
// dependency graph.
//
// Each test round-trips a fully populated upstream value through JSON into
// the SDK type and back, then compares the canonical JSON. A new upstream
// field, a renamed tag or a changed omitempty shows up as a diff here before
// it shows up as silent data loss in a client.
package conformance

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	ra "github.com/webtor-io/rest-api/services"
	libapi "github.com/webtor-io/web-ui/services/libapi"

	webtor "github.com/webtor-io/api-sdk-go"
)

// roundTrip marshals from, unmarshals into a fresh to, re-marshals, and
// compares the two JSON documents structurally.
func roundTrip(t *testing.T, name string, from any, to any) {
	t.Helper()
	a, err := json.Marshal(from)
	if err != nil {
		t.Fatalf("%s: marshal upstream: %v", name, err)
	}
	if err := json.Unmarshal(a, to); err != nil {
		t.Fatalf("%s: unmarshal into SDK type: %v", name, err)
	}
	b, err := json.Marshal(to)
	if err != nil {
		t.Fatalf("%s: marshal SDK type: %v", name, err)
	}
	var am, bm any
	if err := json.Unmarshal(a, &am); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &bm); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(am, bm) {
		t.Errorf("%s: wire drift\nupstream: %s\nsdk:      %s", name, a, b)
	}
}

func upstreamListItem() ra.ListItem {
	return ra.ListItem{
		ID:          "abc",
		Name:        "movie.mkv",
		PathStr:     "/dir/movie.mkv",
		Type:        ra.ListTypeFile,
		Size:        123456,
		MediaFormat: "video",
		MimeType:    "video/x-matroska",
		Ext:         "mkv",
		Index:       7,
	}
}

func TestResourceResponse(t *testing.T) {
	f := upstreamListItem()
	roundTrip(t, "ResourceResponse", ra.ResourceResponse{
		ID:         "08ada5a7a6183aae1e09d831df6748d566095a10",
		Name:       "Sintel",
		MagnetURI:  "magnet:?xt=urn:btih:08ada5a7a6183aae1e09d831df6748d566095a10",
		MultiFile:  false,
		File:       &f,
		Size:       123456,
		FilesCount: 1,
	}, &webtor.ResourceResponse{})
}

func TestListResponse(t *testing.T) {
	roundTrip(t, "ListResponse", ra.ListResponse{
		ListItem: ra.ListItem{ID: "root", PathStr: "/", Type: ra.ListTypeDirectory, Size: 99},
		Items:    []ra.ListItem{upstreamListItem()},
		Count:    1,
	}, &webtor.ListResponse{})
}

func TestExportResponse(t *testing.T) {
	roundTrip(t, "ExportResponse", ra.ExportResponse{
		Source: upstreamListItem(),
		ExportItems: map[string]ra.ExportItem{
			"download": {
				URL: "https://edge.example.com/x?download=true",
				ExportMetaItem: ra.ExportMetaItem{
					Meta: &ra.ExportMeta{Transcode: true, Multibitrate: true, Cache: true, TranscodeCache: true},
				},
			},
			"stream": {
				URL: "https://edge.example.com/x~hls/index.m3u8",
				ExportStreamItem: ra.ExportStreamItem{
					Tag: &ra.ExportTag{
						Name:    ra.ExportTagNameVideo,
						Preload: ra.ExportPreloadTypeAuto,
						Sources: []ra.ExportSource{{Src: "https://s", Type: "application/x-mpegURL"}},
						Tracks: []ra.ExportTrack{{
							Src: "https://t", Kind: ra.ExportKindTypeSubtitles, SrcLang: "en", Label: "English",
						}},
						Src:    "https://tag-src",
						Alt:    "alt",
						Poster: "https://poster",
					},
				},
			},
		},
	}, &webtor.ExportResponse{})
}

func TestLibraryTypes(t *testing.T) {
	added := time.Date(2026, 1, 2, 15, 4, 5, 0, time.UTC)
	roundTrip(t, "LibraryItem", libapi.LibraryItem{
		ResourceID: "08ada5a7a6183aae1e09d831df6748d566095a10",
		Name:       "Sintel",
		Size:       734003200,
		FilesCount: 3,
		AddedAt:    added,
	}, &webtor.LibraryItem{})
	roundTrip(t, "LibraryListResponse", libapi.LibraryListResponse{
		Items:  []libapi.LibraryItem{{ResourceID: "x", Name: "n", AddedAt: added}},
		Count:  42,
		Limit:  100,
		Offset: 0,
		Type:   "all",
		Sort:   "recent",
	}, &webtor.LibraryListResponse{})
}

func TestVaultTypes(t *testing.T) {
	total, avail := 100.0, 40.0
	created := time.Date(2026, 1, 2, 15, 4, 5, 0, time.UTC)
	pledge := libapi.Pledge{
		PledgeID:   "3fa85f64-5717-4562-b3fc-2c963f66afa6",
		ResourceID: "08ada5a7a6183aae1e09d831df6748d566095a10",
		Name:       "Sintel",
		Amount:     2,
		Frozen:     true,
		Funded:     true,
		Vaulted:    false,
		Expired:    false,
		RequiredVP: 2,
		FundedVP:   2,
		CreatedAt:  created,
	}
	roundTrip(t, "VaultResponse", libapi.VaultResponse{
		Points:  libapi.VaultPoints{Total: &total, Available: &avail, Funded: 60, Frozen: 20, Claimable: 40},
		Content: libapi.VaultContent{Vaulted: 3, Loading: 1, Expiring: 0},
		Pledges: []libapi.Pledge{pledge},
	}, &webtor.VaultResponse{})

	progress, stored, totalSize := 42.5, int64(1073741824), int64(2147483648)
	roundTrip(t, "PledgeStatusResponse", libapi.PledgeStatusResponse{
		Pledge:     pledge,
		Status:     libapi.PledgeStatusStoring,
		Progress:   &progress,
		StoredSize: &stored,
		TotalSize:  &totalSize,
	}, &webtor.PledgeStatusResponse{})
}

func TestProfileTypes(t *testing.T) {
	roundTrip(t, "ProfileResponse", libapi.ProfileResponse{
		UserID:   "3fa85f64-5717-4562-b3fc-2c963f66afa6",
		Email:    "user@example.com",
		Tier:     libapi.ProfileTier{ID: 1, Name: "Pro"},
		Settings: libapi.ProfileSettings{ShowAdult: true},
		Scopes:   []string{"api:read", "api:write"},
	}, &webtor.ProfileResponse{})
}

func TestDeviceTypes(t *testing.T) {
	roundTrip(t, "DeviceCodeResponse", libapi.DeviceCodeResponse{
		DeviceCode:              "6c0b8bad-4b41-4bcb-9d10-4c0a0a8e1e3f",
		UserCode:                "F7KQ-29XD",
		VerificationURI:         "https://webtor.io/device",
		VerificationURIComplete: "https://webtor.io/device?code=F7KQ-29XD",
		ExpiresIn:               600,
		Interval:                5,
	}, &webtor.DeviceCodeResponse{})
	roundTrip(t, "DeviceTokenResponse", libapi.DeviceTokenResponse{
		Key: "99999999-8888-7777-6666-555555555555",
	}, &webtor.DeviceTokenResponse{})
}
