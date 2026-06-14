# deeplink

Short link generation, click analytics, link expiry, and OG preview pages for Go.
Pluggable processors; in-memory, Redis, or SQLite storage; one dependency in the core.

[![CI](https://github.com/yinebebt/deeplink/actions/workflows/ci.yml/badge.svg)](https://github.com/yinebebt/deeplink/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/yinebebt/deeplink.svg)](https://pkg.go.dev/github.com/yinebebt/deeplink)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

## Install

```bash
go get github.com/yinebebt/deeplink
```

## Usage

```go
service, err := deeplink.New(deeplink.Config{
    BaseURL:     "https://link.example.com",
    Store:       deeplink.NewMemoryStore(),
    TemplateDir: "templates/default",
})
if err != nil {
    log.Fatal(err)
}
service.Register(deeplink.RedirectProcessor{})

// Mount alongside your own routes.
mux := http.NewServeMux()
mux.Handle("/", service.Handler())
mux.HandleFunc("GET /hello", yourHandler)

log.Fatal(http.ListenAndServe(":8090", mux))
```

Create a short link:

```bash
curl -X POST http://localhost:8090/shorten \
  -H 'Content-Type: application/json' \
  -d '{"type":"redirect","url":"https://example.com/docs","title":"Docs"}'
```

Open the returned `short_url` in a browser.

## Custom processors

Implement [Processor](https://pkg.go.dev/github.com/yinebebt/deeplink#Processor):

```go
type Processor interface {
    Type() string
    Process(ctx context.Context, link *Link) error
}
```

For custom template data, also implement `Previewer`:

```go
type Previewer interface {
    Preview(link *Link) any
}
```

See [example/custom](example/custom/) for a working custom processor with tests.

## Standalone server

A ready-to-run Redis-backed server is included:

```bash
docker compose up -d
go run ./cmd/deeplink
```

## Stores

Any `Store` implementation works; the destination determines persistence.
All built-in stores carry the full feature set — link expiry and click
analytics included:

| Store | Import | Persistent | Analytics storage |
| --- | --- | --- | --- |
| `deeplink.NewMemoryStore()` | core | no | aggregate maps |
| `redisstore.New(client)` | `deeplink/redisstore` | yes | aggregate hashes (HINCRBY) |
| `sqlitestore.New(dsn)` | `deeplink/sqlitestore` | yes | per-visit event rows |

```go
import "github.com/yinebebt/deeplink/sqlitestore"

store, err := sqlitestore.New("deeplink.db")
```

The Redis and SQLite stores live in subpackages, so their drivers
(`go-redis`, `modernc.org/sqlite`) are only pulled in when you import them —
the core stays at a single dependency (`go-nanoid`). Need another backend?
Implement the [`Store`](https://pkg.go.dev/github.com/yinebebt/deeplink#Store)
interface, including the two analytics methods (`RecordEvents` and `Stats`).

## Link expiry

Set `expires_at` (RFC 3339, must be in the future) to make a link
self-destruct. After it passes, the link resolves as `404` and stores purge
it (Redis via a native TTL, others lazily on read):

```bash
curl -X POST http://localhost:8090/shorten \
  -H 'Content-Type: application/json' \
  -d '{"type":"redirect","url":"https://example.com","expires_at":"2026-12-31T23:59:59Z"}'
```

Omit `expires_at` for a link that never expires.

## Click analytics

Every resolved visit is captured off the redirect path and flushed in the
background — bumping the click counter and recording the event for
breakdowns, which every store supports. Query them at `GET /stats/{shortID}`:

```json
{
  "short_id": "aBcD…",
  "clicks": 1280,
  "by_platform": {"android": 700, "ios": 500, "web": 80},
  "by_referrer": {"t.co": 410, "facebook.com": 260},
  "by_day":      {"2026-06-13": 640, "2026-06-14": 640}
}
```

Platform, device, browser, OS, and referrer are derived from the request.

## HTTP routes

| Method | Path | Description |
| --- | --- | --- |
| POST | `/shorten` | Create a short link |
| PATCH | `/{shortID}` | Update mutable fields on a link |
| DELETE | `/{shortID}` | Soft-delete a link (3h grace) |
| GET | `/{shortID}` | Preview page (or 302 redirect) |
| GET | `/links` | All links across types (dashboard data source) |
| GET | `/links/{type}` | List links by type |
| GET | `/links/{type}/{shortID}` | Link detail with click count |
| GET | `/stats/{shortID}` | Click analytics breakdowns |
| GET | `/health` | Health check |

When any store URL is set (`AndroidStoreURL`, `IOSStoreURL`, `WebFallbackURL`), these are also registered:

| Method | Path | Description |
| --- | --- | --- |
| GET | `/preview/{shortID}` | Preview without auto-redirect |
| GET | `/redirect` | App store redirect by platform |
| GET | `/.well-known/` | Static files from template dir |

For iOS Universal Links and Android App Links, place your `apple-app-site-association`
and `assetlinks.json` files in `<TemplateDir>/.well-known/`.

## Configuration

`cmd/deeplink` reads its configuration from environment variables — see
[`.env.example`](.env.example) for the full list with defaults. Setting
`DEEPLINK_API_KEY` enables auth on the mutating endpoints, sent as
`Authorization: Bearer <key>` or `X-API-Key`.

## Templates

The default templates live in `templates/default/`; copy them and point
`TemplateDir` at the copy to customize. They render the
[preview metadata](#link-preview-metadata) fields plus `{{.ShortURL}}`,
`{{.Lang}}`, and the store-fallback URLs (`{{.AndroidStoreURL}}`,
`{{.IOSStoreURL}}`, `{{.WebFallbackURL}}`).

The default `link.html` tries the destination first (so an installed app
opens via its Universal/App Link) and, after a short timeout, falls back to
the right app store by platform — with no store URLs it just forwards to the
destination.

## Link preview metadata

Preview pages emit Open Graph, Twitter Card, and fediverse tags. Empty fields
are omitted, so scrapers never see `content=""`.

### Per-link fields (`Link`)

| Field | Effect |
| --- | --- |
| `Title` | `<title>`, `og:title`, `twitter:title` |
| `Description` | `description`, `og:description`, `twitter:description` |
| `ImageURL` | `og:image`, `twitter:image`. PNG/JPG/WebP only (SVG fails most scrapers); 1200x630 recommended |
| `ImageWidth` / `ImageHeight` | `og:image:width` / `og:image:height` |
| `ImageAlt` | `og:image:alt`, `twitter:image:alt` |
| `OGType` | `og:type` (defaults to `website`). Setting `article` also emits `article:published_time` and `article:modified_time` |
| `Locale` | `og:locale`. Falls back to `Config.Locale` |
| `UpdatedAt` | `og:updated_time`. Set automatically on create |

### Service-wide fields (`Config`)

| Field | Effect |
| --- | --- |
| `SiteName` | `og:site_name` |
| `Locale` | Default `og:locale` when `Link.Locale` is empty |
| `TwitterSite` | `twitter:site` (e.g. `@example`) |
| `FediverseCreator` | `fediverse:creator` (e.g. `@user@instance.tld`) |

Pages also emit `robots: noindex,follow` so short links don't compete with
the destination URL in search.

## Dashboard

A React + TypeScript dashboard lives in [`web/`](web/), configured by
`VITE_API_URL` (see [`.env.example`](.env.example)).

```bash
cd web && npm install && npm run dev   # http://localhost:5173
```

It reads the API key from `localStorage` (`deeplink.apiKey`) and sends it as
`X-API-Key`, so set `DEEPLINK_API_KEY` to enforce auth. For a cross-origin
deploy, add the origin to `DEEPLINK_ALLOWED_ORIGINS`.

## Development

```bash
go test ./...          # run tests
go run ./cmd/deeplink  # run standalone server (needs Redis)
```

## License

[MIT](LICENSE)
