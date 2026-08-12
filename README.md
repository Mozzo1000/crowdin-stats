# crowdin-stats

Self-hosted service generating live, embeddable SVG badges (translation
progress table + contributor grid) for Crowdin projects, for use in GitHub
READMEs. See `PLAN.md` for the full design and `SECURITY.md` for the
encryption/token-handling guarantees.

## Development

```bash
go build ./...
go test ./...

# regenerate landing-page demo SVGs after changing render.go
go run . gendemo

# rebuild compiled CSS after changing input.css or static/*.html
npx tailwindcss -i input.css -o static/app.css --minify
```

## Running locally

```bash
export MASTER_KEY=$(openssl rand -base64 32)
export DB_PATH=./data/db.sqlite
go run .
```

## Deployment

See `docker-compose.yml`, `Dockerfile`, and `Caddyfile`. Copy `.env.example`
to `.env`, fill in `MASTER_KEY` and `HOST`, then `docker compose up -d --build`.
