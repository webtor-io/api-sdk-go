package webtor

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// BackendKind identifies which of the three supported backends a client
// talks to.
type BackendKind string

const (
	// KindWebUI is webtor.io's account-scoped JSON API (default backend).
	KindWebUI BackendKind = "webui"
	// KindRapidAPI is the RapidAPI marketplace gateway.
	KindRapidAPI BackendKind = "rapidapi"
	// KindDirect is a self-hosted / in-cluster rest-api instance.
	KindDirect BackendKind = "direct"
)

// Default endpoints.
const (
	// DefaultWebUIBaseURL is the public JSON API of webtor.io. The same
	// routes are also mounted at https://webtor.io/api/v1.
	DefaultWebUIBaseURL = "https://api.webtor.io/v1"
	// DefaultRapidAPIBaseURL is the RapidAPI marketplace endpoint.
	DefaultRapidAPIBaseURL = "https://webtor.p.rapidapi.com"
)

// Backend carries everything that differs between the three supported API
// deployments: where to send requests, how to authorize them, and which API
// surfaces exist there.
type Backend interface {
	BaseURL() *url.URL
	// Authorize sets the backend's auth headers on req. It must not read the
	// body and must be safe to call on retried requests.
	Authorize(req *http.Request)
	Capabilities() Capabilities
	Kind() BackendKind
}

type backend struct {
	kind BackendKind
	base *url.URL
	caps Capabilities
	auth func(*http.Request)
}

func (b *backend) BaseURL() *url.URL          { return b.base }
func (b *backend) Authorize(r *http.Request)  { b.auth(r) }
func (b *backend) Capabilities() Capabilities { return b.caps }
func (b *backend) Kind() BackendKind          { return b.kind }

func parseBase(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("webtor: invalid base URL %q: %w", raw, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" || u.Host == "" {
		return nil, fmt.Errorf("webtor: base URL %q must be http(s) and absolute", raw)
	}
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawQuery, u.Fragment = "", ""
	return u, nil
}

// WebUIOption configures the WebUI backend.
type WebUIOption func(*webUIConfig)

type webUIConfig struct {
	baseURL string
}

// WithWebUIBaseURL points the backend at a staging or self-hosted web-ui
// (e.g. "https://example.com/api/v1"). The URL must include the /v1 mount.
func WithWebUIBaseURL(u string) WebUIOption {
	return func(c *webUIConfig) { c.baseURL = u }
}

// WebUI returns the backend for webtor.io's account-scoped JSON API.
// apiKey is the account's API key (issued from the profile page or via the
// device flow). An empty apiKey yields an unauthenticated client that can
// only drive the device flow — every other call fails with unauthorized.
func WebUI(apiKey string, opts ...WebUIOption) (Backend, error) {
	cfg := webUIConfig{baseURL: DefaultWebUIBaseURL}
	for _, o := range opts {
		o(&cfg)
	}
	base, err := parseBase(cfg.baseURL)
	if err != nil {
		return nil, err
	}
	return &backend{
		kind: KindWebUI,
		base: base,
		caps: CapLibrary | CapVault | CapProfile | CapDeviceFlow,
		auth: func(r *http.Request) {
			if apiKey != "" {
				r.Header.Set("Authorization", "Bearer "+apiKey)
			}
		},
	}, nil
}

// RapidAPI returns the backend for the RapidAPI marketplace. key is the
// consumer's X-RapidAPI-Key. Only the resource surface is available.
func RapidAPI(key string) (Backend, error) {
	base, err := parseBase(DefaultRapidAPIBaseURL)
	if err != nil {
		return nil, err
	}
	host := base.Host
	return &backend{
		kind: KindRapidAPI,
		base: base,
		auth: func(r *http.Request) {
			r.Header.Set("X-RapidAPI-Key", key)
			r.Header.Set("X-RapidAPI-Host", host)
		},
	}, nil
}

// DirectOption configures the Direct backend.
type DirectOption func(*directConfig)

type directConfig struct {
	apiKey string
	token  string
}

// WithDirectAPIKey sets the pass-through api-key that rest-api embeds into
// the export URLs it generates (validated downstream, not by rest-api).
func WithDirectAPIKey(k string) DirectOption {
	return func(c *directConfig) { c.apiKey = k }
}

// WithDirectToken sets the pass-through JWT that rest-api embeds into the
// export URLs it generates.
func WithDirectToken(t string) DirectOption {
	return func(c *directConfig) { c.token = t }
}

// Direct returns the backend for a self-hosted or in-cluster rest-api
// instance at baseURL (e.g. "http://localhost:8080"). rest-api itself is
// unauthenticated; the optional credentials are baked into generated export
// URLs and checked further down the chain.
func Direct(baseURL string, opts ...DirectOption) (Backend, error) {
	cfg := directConfig{}
	for _, o := range opts {
		o(&cfg)
	}
	base, err := parseBase(baseURL)
	if err != nil {
		return nil, err
	}
	return &backend{
		kind: KindDirect,
		base: base,
		auth: func(r *http.Request) {
			if cfg.apiKey != "" {
				r.Header.Set("X-Api-Key", cfg.apiKey)
			}
			if cfg.token != "" {
				r.Header.Set("X-Token", cfg.token)
			}
		},
	}, nil
}
