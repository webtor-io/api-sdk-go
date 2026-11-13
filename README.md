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
  `Code` (`not_found`, `payment_required`, `rate_limited`, …) and, where the
  server refines it, a `Reason`, regardless of the backend's wire dialect.
  Predicates: `webtor.IsNotFound(err)` etc.
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

## Private torrents

A private torrent (BEP 27 private flag, `ResourceResponse.Private`) plays only
for the account that added it with its own tracker. `ResourceResponse.Verified`
is `"accepted"` when a magnet with `tr=` for an already stored private torrent
passed the tracker check.

Refusals carry their own codes on every backend:

| `Code` | Status | When | Constant |
|---|---|---|---|
| `private_torrent` | 403 | the caller may not read this torrent; `Reason` says why | `CodePrivateTorrent` |
| `private_torrent_file` | 403 | `TorrentFile` of a private torrent: its `.torrent` is never served | `CodePrivateTorrentFile` |
| `private_needs_account` | 422 | adding a private torrent without an account | `CodePrivateNeedsAccount` |
| `private_no_tracker` | 422 | the private torrent names no usable tracker | `CodePrivateNoTracker` |
| `private_too_many_trackers` | 422 | the private torrent names more trackers than the server accepts | `CodePrivateTooManyTrackers` |
| `private_magnet` | 422 | a magnet of a private torrent without `tr=`: add the `.torrent` file | `CodePrivateMagnet` |
| `private_tracker_refused` | 422 | the tracker refused the key in a magnet's `tr=`; `Reason` is the refusal class | `CodePrivateTrackerRefused` |
| `private_verify_unavailable` | 503 | the tracker check could not run; wait `RetryAfter`, the SDK does not repeat the add itself | `CodePrivateVerifyUnavailable` |
| `private_unavailable` | 422 on add, 403 on access | this server does not support private torrents | `CodePrivateUnavailable` |

`Reason` of `private_torrent`: `sign_in` (`webtor.ReasonSignIn`, the caller is
anonymous), `no_record` (`webtor.ReasonNoRecord`, the account has not added
this torrent with its own tracker) or `token_in_query`. Of
`private_tracker_refused`: `key`, `account`, `unregistered`, `client` or
`other`.

```go
_, err := c.Resource(ctx, id)
var ae *webtor.Error
if errors.As(err, &ae) && ae.Code == webtor.CodePrivateTorrent {
    switch ae.Reason {
    case webtor.ReasonSignIn:
        // sign in, then try again
    case webtor.ReasonNoRecord:
        // add the .torrent file from your tracker with this account
    }
}
```

A private refusal is not `forbidden`: `webtor.IsForbidden(err)` is false for
it, so branch on `Code`.

Export URLs of a private torrent carry a token bound to the account and live at
most 24 hours. Never store them; `OpenDownload` re-resolves them by itself when
resuming. With the `Direct` backend, private torrents need `WithDirectToken`
with a token that carries a `uid` claim; without it they are refused.

## License

MIT
