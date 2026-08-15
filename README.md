# Crowdin Stats

Generates live, embeddable SVG images (translation progress table +
contributor grid) for Crowdin projects, for use in GitHub READMEs.

A hosted instance is available at https://crowdin-stats.rewake.org — sign in
with a Crowdin project token to get your embed URLs in under a minute. You
can also run your own instance; see [Deployment](#deployment) below.

<a href="https://rewake.org">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/Mozzo1000/rewake.org/main/assets/badge-dark.svg">
    <img src="https://raw.githubusercontent.com/Mozzo1000/rewake.org/main/assets/badge.svg" alt="a re:wake project">
  </picture>
</a>

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

## Environment variables

| Variable     | Required | Default              | Description                                                                                   |
| ------------ | -------- | --------------------- | ----------------------------------------------------------------------------------------------- |
| `MASTER_KEY` | Yes      | —                      | Secret key used to encrypt stored Crowdin project tokens. Generate one with `openssl rand -base64 32` and never commit it. |
| `HOST`       | Yes      | —                      | The public hostname this instance is served from (e.g. `crowdin-stats.example.com`), used to build embed/setup URLs. |
| `DB_PATH`    | No       | `./data/db.sqlite`     | Path to the SQLite database file.                                                              |

## Deployment

The easiest way to run your own instance is with Docker Compose. The app
image is published on Docker Hub as
[`mozzo/crowdin-stats`](https://hub.docker.com/r/mozzo/crowdin-stats),
and the included `Caddyfile` handles HTTPS automatically, so there's nothing
extra to configure for certificates.

1. Download `docker-compose.yml`, `Caddyfile`, and `.env.example` onto the
   server you want to deploy to:

   ```bash
   curl -O https://raw.githubusercontent.com/Mozzo1000/crowdin-stats/main/docker-compose.yml \
        -O https://raw.githubusercontent.com/Mozzo1000/crowdin-stats/main/Caddyfile \
        -O https://raw.githubusercontent.com/Mozzo1000/crowdin-stats/main/.env.example
   ```
2. Point a DNS record at that server for the domain you'll use.
3. Copy `.env.example` to `.env` and fill in `MASTER_KEY` (a random secret
   used to encrypt stored Crowdin tokens — generate one with
   `openssl rand -base64 32`) and `HOST` (the domain from step 2).
4. Create the data folder and make it writable by the app's user:
   `mkdir -p data && sudo chown 10001:10001 data`.
5. Start it up: `docker compose up -d`.

That's it — Caddy will fetch a TLS certificate for your domain and the app
will be reachable at `https://<your HOST>`.

The `app` service in `docker-compose.yml` looks like this:

```yaml
services:
  app:
    image: mozzo/crowdin-stats:latest
    environment:
      - MASTER_KEY=${MASTER_KEY}
      - DB_PATH=/data/db.sqlite
      - HOST=${HOST}
    volumes:
      - ./data:/data
    restart: unless-stopped
```

To update to a newer release later, run `docker compose pull && docker
compose up -d`.

## License

Apache License 2.0 — see [LICENSE](LICENSE).
