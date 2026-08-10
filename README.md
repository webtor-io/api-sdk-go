# webtor api-sdk-go

Go SDK for the [webtor.io](https://webtor.io) JSON API: store torrents,
list their content, resolve streaming/download URLs, manage the account
library and Vault — over any of the three API deployments with one client.

```go
import webtor "github.com/webtor-io/api-sdk-go"

backend, _ := webtor.WebUI(os.Getenv("WEBTOR_API_KEY"))
c, _ := webtor.New(backend)

res, _ := c.AddResource(ctx, webtor.Magnet("magnet:?xt=urn:btih:..."))
resp, _ := c.Export(ctx, res.ID, res.File.ID, webtor.ExportOptions{
    Types: []webtor.ExportType{webtor.ExportTypeDownload},
})
url, _ := resp.DownloadURL() // short-lived, self-authorizing — use immediately
```

## Backends

| Constructor | Deployment | Auth | Surface |
|---|---|---|---|
| `WebUI(apiKey)` | `https://api.webtor.io/v1` (paid plans) | `Authorization: Bearer` | resources + library, vault, profile, device flow |
| `RapidAPI(key)` | `https://webtor.p.rapidapi.com` | `X-RapidAPI-Key` | resources |
| `Direct(baseURL)` | your own [self-hosted](https://github.com/webtor-io/self-hosted) rest-api | optional pass-through | resources |

The paths, parameters and response types are identical across all three —
only auth and the error dialect differ, and the SDK normalizes both.
`Client.Supports(webtor.CapLibrary)` tells you what the configured backend
offers; unsupported calls fail fast with a `*CapabilityError`.

## Highlights

- **Normalized errors** — every non-2xx becomes `*webtor.Error` with a stable
  `Code` (`not_found`, `payment_required`, `rate_limited`, …) regardless of the
  backend's wire dialect. Predicates: `webtor.IsNotFound(err)` etc.
- **Device-flow login** — `StartDeviceAuth` + `WaitDeviceToken` implement the
  RFC 8628 polling protocol; the person confirms a short code on webtor.io and
  the SDK returns a fresh per-device API key. See `examples/device-login`.
- **Resumable downloads** — `OpenDownload` returns an `io.ReadCloser` that
  transparently re-resolves the expiring export URL and resumes with a Range
  request on mid-stream failures.
- **Rate-limit aware** — automatic `Retry-After`-honoring backoff on 429.
- **Zero dependencies** — the SDK's `go.mod` has no third-party requirements.

More runnable examples in [`examples/`](examples/). The wire types are pinned
to the upstream services by the [`conformance/`](conformance/) test module.

## License

MIT
