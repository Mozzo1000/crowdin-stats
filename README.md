# Crowdin Stats

Generates live, embeddable SVG images (translation progress table +
contributor grid) for Crowdin projects, for use in GitHub READMEs.

A hosted instance is available at https://crowdin-stats.rewake.org — sign in
with a Crowdin project token to get your embed URLs in under a minute. You
can also run your own instance; see [Deployment](#deployment) below.

<p>
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="static/demo-table-dark.svg">
    <img src="static/demo-table.svg" alt="Translation progress table example" height="192">
  </picture>
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="static/demo-overall-dark.svg">
    <img src="static/demo-overall.svg" alt="Overall progress card example" height="192">
  </picture>
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="static/demo-overall-circle-dark.svg">
    <img src="static/demo-overall-circle.svg" alt="Overall progress circle example" height="192">
  </picture>
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="static/demo-contributors-dark.svg">
    <img src="static/demo-contributors.svg" alt="Contributor grid example" height="192">
  </picture>
</p>

## Development

```bash
go build ./...
go test ./...

# regenerate landing-page demo SVGs after changing render.go
go run ./cmd/crowdin-stats gendemo

# rebuild compiled CSS after changing input.css or static/*.html
npx tailwindcss -i input.css -o static/app.css --minify
```

## Running locally

```bash
export MASTER_KEY=$(openssl rand -base64 32)
export DB_PATH=./data/db.sqlite
export HOST=localhost:8080
go run ./cmd/crowdin-stats -insecure-http

# add -no-cache to bypass the 12h embed cache entirely — every embed
# request does a live Crowdin fetch, useful while testing
go run ./cmd/crowdin-stats -insecure-http -no-cache
```

`-insecure-http` builds embed/setup URLs with `http://` instead of `https://`,
since running the binary directly (without Caddy terminating TLS in front of
it, as in production) means there's nothing listening on HTTPS locally.
Without it, the browser can't load the `https://` embed URLs the app hands
back and requests to them fail. Omit the flag if you're running behind Caddy
locally too (e.g. via `docker compose`).

## Deployment

See `docker-compose.yml`, `Dockerfile`, and `Caddyfile`. Copy `.env.example`
to `.env`, fill in `MASTER_KEY` and `HOST`, then `docker compose up -d --build`.

The app container runs as a non-root user (uid/gid `10001`) with all Linux
capabilities dropped, so the bind-mounted `./data` directory must be writable
by that uid — run `mkdir -p data && sudo chown 10001:10001 data` before the
first `docker compose up`.

## License

Apache License 2.0 — see [LICENSE](LICENSE).
