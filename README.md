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
| GET | `/{shortID}` | Preview page (or 302 redirect) |
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

## Templates

The default templates in `templates/default/` use these fields from `Link`:

| Field | Template variable | Used for |
| --- | --- | --- |
| URL | `{{.URL}}` | Redirect target, og:url |
| Title | `{{.Title}}` | Page title, og:title |
| Description | `{{.Description}}` | og:description |
| ImageURL | `{{.ImageURL}}` | og:image |

To customize, copy `templates/default/` and set `TemplateDir` in config.

## Development

```bash
go test ./...          # run tests
go run ./cmd/deeplink  # run standalone server (needs Redis)
```

## License

[MIT](LICENSE)
