# deeplink

Short link generation, click tracking, and OG preview pages for Go.
Pluggable processors, Redis or in-memory storage, two dependencies.

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

Environment variables for `cmd/deeplink`:

| Variable | Default | Description |
| --- | --- | --- |
| `DEEPLINK_LISTEN_ADDR` | `:8090` | Listen address |
| `DEEPLINK_BASE_URL` | `http://localhost:8090/` | Base URL for short links |
| `DEEPLINK_REDIS_ADDR` | `localhost:6379` | Redis address |
| `DEEPLINK_REDIS_PASSWORD` | | Redis password |
| `DEEPLINK_ALLOWED_ORIGINS` | | CORS origins (comma-separated) |
| `DEEPLINK_TEMPLATE_DIR` | `templates/default` | Template directory |
| `DEEPLINK_SKIP_PATHS_FILE` | | Skip-path regex file |
| `DEEPLINK_CLICK_BUFFER_SIZE` | `1024` | Async click event buffer capacity |
| `DEEPLINK_CLICK_FLUSH_INTERVAL` | `1s` | How often buffered clicks are flushed to the store |
| `DEEPLINK_API_KEY` | | Protect mutating endpoints (`Authorization: Bearer <key>` or `X-API-Key: <key>`) |
| `DEEPLINK_SITE_NAME` | | `og:site_name` on every preview |
| `DEEPLINK_LOCALE` | `en_US` | Default `og:locale` (per-link `locale` overrides) |
| `DEEPLINK_TWITTER_SITE` | | `twitter:site` (e.g. `@example`) |
| `DEEPLINK_FEDIVERSE_CREATOR` | | `fediverse:creator` (e.g. `@user@instance.tld`) |

## Templates

The default templates in `templates/default/` use these fields from `Link`:

| Field | Template variable | Used for |
| --- | --- | --- |
| URL | `{{.URL}}` | Redirect target |
| Title | `{{.Title}}` | Page title, og:title, twitter:title |
| Description | `{{.Description}}` | og:description, twitter:description |
| ImageURL | `{{.ImageURL}}` | og:image, twitter:image |
| ImageWidth | `{{.ImageWidth}}` | og:image:width |
| ImageHeight | `{{.ImageHeight}}` | og:image:height |
| ImageAlt | `{{.ImageAlt}}` | og:image:alt, twitter:image:alt |
| OGType | `{{.OGType}}` | og:type (defaults to `website`) |
| Locale | `{{.Locale}}` | og:locale (per-link override) |
| Lang | `{{.Lang}}` | `<html lang>` (derived from Locale) |
| CreatedAt | `{{.CreatedAt}}` | article:published_time |
| UpdatedAt | `{{.UpdatedAt}}` | og:updated_time, article:modified_time |
| ShortURL | `{{.ShortURL}}` | canonical link, og:url |

To customize, copy `templates/default/` and set `TemplateDir` in config.

## Link preview metadata

Preview pages emit standard Open Graph, Twitter Card, and fediverse meta
tags consumed by every platform that scrapes link previews. Every tag is
gated on its source value: empty fields are not emitted, so scrapers never
see `content=""` warnings.

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

### Example payload

```bash
curl -X POST http://localhost:8090/shorten \
  -H 'Content-Type: application/json' \
  -d '{
    "type": "redirect",
    "url": "https://example.com/posts/launch",
    "title": "We just launched",
    "description": "What is new in v2",
    "image_url": "https://cdn.example.com/og/launch.png",
    "image_width": 1200,
    "image_height": 630,
    "image_alt": "v2 launch cover",
    "og_type": "article"
  }'
```

Preview pages also emit `<meta name="robots" content="noindex,follow">` so
short links do not compete with the destination URL in search rankings.

## Dashboard

A standalone React + TypeScript dashboard lives in [`web/`](web/). It
talks to the Go service over HTTP and shares the repo's root `.env`.

```bash
cd web
npm install
npm run dev     # http://localhost:5173
```

One env var configures it (in root `.env`):

```
VITE_API_URL=http://localhost:8091
```

- `npm run dev` uses it as the Vite proxy target only — the browser hits
  `:5173`, so there is no CORS and the scheme is optional.
- `npm run build` bakes it into the bundle as an absolute base, so the
  static dist can be hosted on any origin. Scheme required.

The dashboard reads the API key from `localStorage` (`deeplink.apiKey`)
and sends it as `X-API-Key` on mutating requests, so set
`DEEPLINK_API_KEY` server-side to enable enforcement. When the SPA runs
on a different origin in production, add that origin to
`DEEPLINK_ALLOWED_ORIGINS`.

## Development

```bash
go test ./...          # run tests
go run ./cmd/deeplink  # run standalone server (needs Redis)
```

## License

[MIT](LICENSE)
