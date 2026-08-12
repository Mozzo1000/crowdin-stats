# crowdin-stats — Implementation Plan

## 1. Purpose

A self-hosted service that generates two embeddable, always-live SVG images per
registered Crowdin project, for use in GitHub READMEs:

- **`table.svg`** — per-language translation progress bars
- **`contributors.svg`** — grid of Crowdin translators/proofreaders, weighted by
  contribution (similar to [contrib.rocks](https://contrib.rocks))

Design priorities, in order:

1. **Operator cannot read stored Crowdin tokens** (encryption at rest, key never
   in the database)
2. **Onboarding takes under a minute**, no user account/login required
3. **Simple to run**: single Go binary, SQLite, docker-compose, self-hosted on
   an EU VM — no cloud KMS, no Redis, no external dependencies beyond Crowdin's
   API itself
4. **Long cache (12h)** with stale-while-revalidate, so badge requests are fast
   and Crowdin API usage stays low

---

## 2. Architecture

```
                         ┌─────────────────────────────┐
  README <img src> ─────▶│  Caddy (TLS termination)    │
                         └──────────────┬──────────────┘
                                        │
                         ┌──────────────▼──────────────┐
                         │  Go app (single binary)      │
                         │  ┌────────────────────────┐  │
                         │  │ GET  /setup (HTML page) │  │
                         │  │ POST /setup (onboard)   │  │
                         │  │ GET  /badge/:id/table   │  │
                         │  │ GET  /badge/:id/contrib │  │
                         │  └────────────────────────┘  │
                         │  in-memory/SQLite rate limiter│
                         │  background refresh goroutines│
                         └──────────────┬──────────────┘
                                        │
                         ┌──────────────▼──────────────┐
                         │  SQLite (single file, WAL)   │
                         │  - projects (encrypted token) │
                         │  - cache (rendered SVGs)      │
                         │  - rate_limits                │
                         └──────────────────────────────┘
                                        │
                         ┌──────────────▼──────────────┐
                         │  Crowdin REST API v2          │
                         │  via official Go client        │
                         │  github.com/crowdin/            │
                         │  crowdin-api-client-go           │
                         └───────────────────────────────┘
```

Two containers total: `app` (Go) and `caddy` (reverse proxy/TLS). SQLite lives
on a mounted volume. No Redis, no KMS, no message queue.

---

## 3. Tech stack

| Layer               | Choice                                                          |
|---------------------|-------------------------------------------------------------------|
| Language             | Go                                                                |
| HTTP router          | `chi` (or stdlib `net/http` + `http.ServeMux` if preferred)       |
| Crowdin API client    | **Official library**: `github.com/crowdin/crowdin-api-client-go` — do not hand-roll HTTP calls to Crowdin |
| DB                    | SQLite via `modernc.org/sqlite` (pure Go, no cgo — simpler static builds) |
| Encryption             | `golang.org/x/crypto/nacl/secretbox`                              |
| Cache                  | SQLite table (`cache`), 12h TTL, no Redis needed at this scale     |
| Reverse proxy / TLS     | Caddy (automatic HTTPS)                                            |
| Background refresh       | Go goroutines + `time.Ticker`, no external cron                    |
| Deployment                | docker-compose, two services (`app`, `caddy`), single EU VM         |
| Onboarding UI               | Plain HTML + vanilla JS, served as a static file by the Go binary   |

---

## 4. Data model (SQLite)

```sql
PRAGMA journal_mode = WAL;

CREATE TABLE projects (
    public_id           TEXT PRIMARY KEY,      -- random UUIDv4, used in badge URLs
    crowdin_project_id  TEXT NOT NULL,          -- plaintext; not a credential, see §5
    ciphertext           BLOB NOT NULL,          -- NaCl secretbox output
    nonce                 BLOB NOT NULL,          -- 24 bytes, unique per encryption
    key_version            INTEGER NOT NULL DEFAULT 1,  -- see §5 (key rotation)
    created_at               INTEGER NOT NULL,
    revoked                   INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE cache (
    key         TEXT PRIMARY KEY,   -- e.g. "table:{public_id}" or "contrib:{public_id}:limit=30:unit=words"
    svg         TEXT NOT NULL,
    cached_at   INTEGER NOT NULL,
    expires_at  INTEGER NOT NULL
);

CREATE TABLE rate_limits (
    bucket_key   TEXT PRIMARY KEY,   -- e.g. "setup:{ip}" or "refresh:{public_id}"
    count        INTEGER NOT NULL,
    window_start INTEGER NOT NULL
);
```

Notes:
- No user/account table, no login. `public_id` unguessability is the access
  control for badge URLs.
- `revoked` flag lets a project be disabled without deleting the row
  (audit trail preserved until a user explicitly requests deletion by email).
- Cache `key` must encode all query parameters that affect rendering
  (see §7), otherwise different parameterized requests would collide on the
  same cached SVG.
- `crowdin_project_id` is intentionally **not** encrypted — it's required
  unencrypted to call the API and is not itself a credential. See §5 for why
  this is a deliberate choice, not an inconsistency, and what it means for
  users with private Crowdin projects.
- `key_version` exists from day one so `MASTER_KEY` rotation is possible
  later without a breaking migration — see §5.4.
- Expired `cache` rows are not deleted automatically by TTL alone (a stale
  row is simply overwritten on its next successful fetch for that exact
  key). A periodic sweep is required to bound table growth — see §15.2.

---

## 5. Encryption design (the no-knowledge guarantee)

**Primitive:** `golang.org/x/crypto/nacl/secretbox` (XSalsa20-Poly1305,
authenticated encryption).

**Key handling:**
- `MASTER_KEY` — 32 random bytes, base64-encoded, generated once via
  `openssl rand -base64 32`.
- Stored **only** in the VM's environment (`.env` file, `chmod 600`, gitignored),
  injected into the container via docker-compose.
- Never written to SQLite, never logged, never included in any HTTP response,
  never sent to an error-tracking/observability tool.
- Loaded into a Go `[32]byte` once at process startup.

```go
func loadMasterKey() [32]byte {
    raw := os.Getenv("MASTER_KEY")
    if raw == "" {
        log.Fatal("MASTER_KEY not set")
    }
    decoded, err := base64.StdEncoding.DecodeString(raw)
    if err != nil || len(decoded) != 32 {
        log.Fatal("MASTER_KEY must be 32 bytes, base64-encoded")
    }
    var key [32]byte
    copy(key[:], decoded)
    return key
}

func encryptToken(key [32]byte, token string) (ciphertext, nonce []byte, err error) {
    var n [24]byte
    if _, err := rand.Read(n[:]); err != nil {
        return nil, nil, err
    }
    ct := secretbox.Seal(nil, []byte(token), &n, &key)
    return ct, n[:], nil
}

func decryptToken(key [32]byte, ciphertext, nonce []byte) (string, error) {
    var n [24]byte
    copy(n[:], nonce)
    out, ok := secretbox.Open(nil, ciphertext, &n, &key)
    if !ok {
        return "", errors.New("decryption failed — data may be corrupted")
    }
    return string(out), nil
}
```

**Honest guarantee statement** (for `SECURITY.md`):

> Tokens are encrypted at rest using NaCl secretbox. The encryption key exists
> only in the server's runtime environment and is never written to disk in the
> database, logs, or backups. A copy of the database alone is insufficient to
> recover any token. The running application decrypts tokens transiently, in
> memory, only at the moment of making an authenticated request to Crowdin's
> API on your behalf.
>
> The Crowdin Project ID associated with your token is stored in plaintext.
> This is a deliberate choice, not an oversight: the ID is required
> unencrypted to make API calls, and it is not itself a credential — knowing
> a project ID grants no access without a valid token. However, if your
> Crowdin project is private, be aware that our database holds an
> unencrypted association between your badge's `public_id` and your private
> project's identity. Public projects have no such exposure, since their IDs
> are already visible in Crowdin's own public URLs.

**Logging hygiene:** `/setup` (POST) must be excluded from any request/response
body logging middleware — this is the one code path where a plaintext token
transits the server. Add a test asserting this route is never logged.

**Request size limiting:** `/setup` must wrap the incoming request body in
`http.MaxBytesReader` (e.g. 8KB — comfortably larger than any real token +
project ID pair) **before** JSON decoding and **before** the rate limiter
runs. Without this, an oversized POST body can consume memory or CPU during
decoding regardless of rate-limit state, since the limiter only bounds
*request count*, not the cost of a single request.

```go
func handleSetup(w http.ResponseWriter, r *http.Request) {
    r.Body = http.MaxBytesReader(w, r.Body, 8*1024)

    ip := clientIP(r)
    if rateLimited(db, "setup:"+ip, 5, time.Hour) {
        http.Error(w, "too many setup attempts, try again later", 429)
        return
    }
    // ... decode, validate, encrypt, store
}
```

### 5.4 Key rotation

`MASTER_KEY` has no rotation mechanism in v1 beyond the `key_version` column
reserved in the schema (§4). Concretely:

- If `MASTER_KEY` is ever suspected compromised, generating a new key alone
  is **not sufficient** — every row encrypted under the old key becomes
  permanently undecryptable unless a migration re-encrypts them.
- A real rotation path (deferred, but the schema doesn't block it later):
  load both old and new keys at startup, decrypt with `key_version`-selected
  key, re-encrypt with the new key, bump `key_version`, on a rolling basis
  (e.g. lazily on next successful decrypt, or via a one-off migration
  script).
- **v1 documented limitation**: state plainly in `SECURITY.md` that key
  rotation currently requires all users to re-onboard (re-paste their
  token), and that this is a known gap, not a silent one.

---

## 6. Crowdin API access — official Go client

Use **`github.com/crowdin/crowdin-api-client-go`** for all Crowdin API calls.
Do not hand-roll `net/http` requests to Crowdin.

```go
import (
    "github.com/crowdin/crowdin-api-client-go/crowdin"
    "github.com/crowdin/crowdin-api-client-go/crowdin/model"
)

client, err := crowdin.NewClient(token)
```

Required functionality to map onto the client's services (verify exact method
names against the library's current `Projects`, `TranslationStatus`, and
`Reports` service signatures — do not assume the raw-HTTP sketch in earlier
drafts matches 1:1):

1. **Token validation at onboarding** — fetch the single project
   (`client.Projects.Get` or equivalent) to confirm the token is valid and
   scoped to the given project ID before storing anything.
2. **Language progress** (`table.svg` data source) — project-level translation
   progress per language, via the `TranslationStatus` service
   (e.g. `GetProjectProgress` or equivalent — confirm exact method).
3. **Top Members report** (`contributors.svg` data source) — via the `Reports`
   service. This is an **asynchronous** operation in Crowdin's API:
   - Generate report (`name: "top-members"`, `schema.unit` = user-selected
     `words | strings | characters`)
   - Poll status until `finished`
   - Download resulting report data
   - Confirm the official client exposes this as a generate → poll → download
     sequence (e.g. `client.Reports.GenerateReport`, `client.Reports.CheckStatus`,
     `client.Reports.Download`) rather than assuming method names.

**Error handling:** the official client returns either a generic error or a
typed `*model.ValidationErrorResponse` for validation failures — use a type
assertion where distinguishing these matters (e.g. surfacing a clear "token
invalid or insufficient scope" message during onboarding).

---

## 7. Routes

### `GET /`
Serves the landing page (see §9.2).

### `GET /setup`
Serves the onboarding form page (see §9.3).

### `POST /setup`
Onboarding endpoint.

**Request body:**
```json
{ "crowdin_project_id": "123456", "token": "<pasted PAT>" }
```

**Behavior:**
1. Wrap body in `http.MaxBytesReader` (8KB) — see §5.
2. Rate-limit check: `setup:{client_ip}`, e.g. 5/hour (see §8). This order is
   required: size-limit first (cheapest check), then rate limit (before any
   Crowdin API call is made), then validation against Crowdin.
3. Validate the token against the given project ID via the official client —
   reject with a clear error if invalid/wrong scope before storing anything.
4. Encrypt the token (§5); drop the plaintext reference immediately after.
5. Generate a `public_id` (UUIDv4), insert into `projects`.
6. Return badge URLs and ready-to-paste markdown.

**Response:**
```json
{
  "table_url": "https://badges.example.eu/badge/{public_id}/table.svg",
  "contributors_url": "https://badges.example.eu/badge/{public_id}/contributors.svg?limit=30&unit=words",
  "markdown": "![Translation Progress](...)\n![Contributors](...)"
}
```

### `GET /badge/{public_id}/table.svg`
Renders the progress table. No query parameters.

- Cache key: `table:{public_id}`
- 404 if `revoked = 1` or `public_id` not found.
- Cache hit + fresh → serve immediately.
- Cache hit + stale (>12h) → serve stale immediately, trigger background
  refresh (subject to rate limit).
- Cache miss (first-ever request) → rate-limit check, then block on a live
  fetch + render + cache write.

### `GET /badge/{public_id}/contributors.svg?limit=30&unit=words`
Renders the contributor grid.

**Query parameters:**
- `limit` — integer, default `30`, clamped to `[1, 100]`.
- `unit` — one of `words | strings | characters`, default `words`. Maps
  directly to the Top Members report's `schema.unit`.

**Cache key must include both parameters:**
```
contrib:{public_id}:limit={limit}:unit={unit}
```
This prevents different parameter combinations from colliding on the same
cached SVG. Same fresh/stale/cold-cache behavior as `table.svg`.

**Empty state:** if a project has zero contributors on record (common for new
or very small projects), `renderContributorsSVG` must return a small,
explicit "no contributors yet" SVG rather than an empty/zero-height image —
an empty `<svg>` renders as a confusing blank gap in a README.

### `GET /healthz`
Plain liveness check — returns `200 OK` with no body if the process is up
and can reach its own SQLite file (a trivial `SELECT 1` is enough; this
should **not** call out to Crowdin). Used by Caddy/an uptime monitor, not
customer-facing. No rate limiting needed on this route.

---

## 8. Rate limiting

Two independent limiter scopes, since the abuse shapes differ:

| Target                          | Threat                                                        | Limit                          |
|-----------------------------------|-------------------------------------------------------------------|-------------------------------------|
| `POST /setup`                       | Spam registrations / brute-forcing project IDs against stolen tokens | Per-IP: 5/hour                        |
| Cache-miss path on badge routes       | Forcing repeated live Crowdin calls, exhausting API quota            | Per-`public_id`: 20 refreshes/hour     |

**Only the cache-miss / refresh path is rate-limited on badge routes** — normal
cache-hit traffic (the overwhelming majority of real README views) is never
throttled.

**Global outbound limiter (Crowdin's own API limits):** the per-project
limits above bound *individual* project abuse, but don't protect against
aggregate load if many projects' caches happen to expire around the same
time (e.g. shortly after a bulk-import launch). Crowdin enforces its own
API rate limits; add a lightweight global token-bucket (e.g.
`golang.org/x/time/rate`) around all outbound Crowdin calls, with sane
concurrency (e.g. cap concurrent in-flight Crowdin requests), and back off
on 429 responses from Crowdin itself. This matters more as the number of
registered projects grows past a handful — worth a note in the deployment
checklist to revisit if usage scales.

**CSRF:** not applicable in v1 — `/setup` has no session/cookie-based
authentication state to forge, so there's no CSRF-protectable action to
target. Noted explicitly here so it's clear this was considered, not
overlooked.

```go
func rateLimited(db *sql.DB, bucketKey string, limit int, window time.Duration) bool {
    now := time.Now().Unix()
    windowStart := now - int64(window.Seconds())

    var count int
    var storedStart int64
    err := db.QueryRow(`SELECT count, window_start FROM rate_limits WHERE bucket_key = ?`,
        bucketKey).Scan(&count, &storedStart)

    if err == sql.ErrNoRows || storedStart < windowStart {
        db.Exec(`INSERT INTO rate_limits (bucket_key, count, window_start) VALUES (?, 1, ?)
                 ON CONFLICT(bucket_key) DO UPDATE SET count=1, window_start=excluded.window_start`,
            bucketKey, now)
        return false
    }
    if count >= limit {
        return true
    }
    db.Exec(`UPDATE rate_limits SET count = count + 1 WHERE bucket_key = ?`, bucketKey)
    return false
}
```

**Cleanup:** hourly goroutine ticker sweeps `rate_limits` rows older than 2
hours so the table doesn't grow unbounded.

**IP extraction:** trust `X-Forwarded-For` (set by Caddy), fall back to
`RemoteAddr`.

---

## 9. Web interface

Two routes make up the entire public web surface. Both are static HTML,
Tailwind (via CDN or a compiled stylesheet — see §9.4), vanilla JS, no
frontend framework/build step, served directly by the Go binary.

```
GET  /         → landing page (marketing + explanation + live examples)
GET  /setup     → onboarding form
POST /setup      → onboarding submission (JSON API, see §7)
```

### 9.1 Design direction

This is a developer tool, not a consumer product — the interface should read
like something a maintainer would trust to hold an API token, not like a
SaaS landing page selling a subscription. Concretely:

**Palette**
| Token       | Hex       | Use                                          |
|-------------|-----------|-----------------------------------------------|
| `ink`         | `#0B0E14`  | Page background                                |
| `surface`      | `#12161F`  | Card/panel backgrounds, slightly lifted off `ink` |
| `text`          | `#E8EAED`  | Primary text                                     |
| `text-muted`     | `#8B93A3`  | Secondary text, captions                          |
| `border`          | `#232834`  | Hairline borders, dividers                         |
| `accent-mint`       | `#7DD3A8`  | Progress bars, success states, primary signature color |
| `accent-amber`        | `#F5A623`  | Single CTA color — used sparingly, primary button only |

Reasoning: near-black rather than the common warm-cream/serif default, since
this tool's actual output (SVG progress bars, terminal-style JSON) already
has a natural "dark editor" register — the landing page should look like it
belongs next to the artifact it produces, not like a marketing site bolted
on top of a dev tool.

**Typography**
- **Inter** — headings and body copy. Quiet, legible, no display-serif
  moment; this isn't a lifestyle brand.
- **JetBrains Mono** — anything data-shaped: badge URLs, the Project ID/Token
  input fields, code blocks, the markdown output snippet. Using monospace
  specifically for these fields (not just code blocks) reinforces "this is a
  technical credential field," subtly cueing the user to treat it carefully.

**Signature element**: the hero does not open with a headline-and-CTA
template. It opens with a **live, real rendering of `table.svg` and
`contributors.svg`** side by side — actual SVG output from a demo project (or
the operator's own project), not a mockup. The single most convincing thing
this tool can say is "here is exactly what you'll get," shown, not described.
Headline and CTA sit beside/below this, not above it.

**Motion**: minimal. A subtle fade/slide-in on scroll for the "how it works"
steps is acceptable; nothing ambient or looping. A tool that handles API
tokens should feel calm, not flashy.

**Icons**: inline SVG only (Heroicons or Lucide, outline style, stroke-based
— matches the mint/amber accent system cleanly at 1.5px stroke). No icon
font, no external icon service call at runtime.

### 9.2 Landing page (`GET /`)

Single-column, generous vertical rhythm, sections in this order:

1. **Hero**
   - Small eyebrow label: `crowdin-stats`
   - Headline (plain, specific, not clever) — e.g. "Live translation
     progress for your README, without exposing your Crowdin token."
   - One-sentence subhead explaining the mechanism in plain terms: paste a
     token once, get two SVG URLs, embed them anywhere.
   - **Example row**: `table.svg` and `contributors.svg` outputs, rendered at
     real size, in a bordered "this is a README" frame (e.g. a mock
     markdown-file chrome) so visitors immediately see it in context.
     **These are hardcoded static demo SVGs checked into `static/`, not a
     live call to any real or demo `public_id`** — the landing page must
     render instantly with no backend dependency, and a static asset avoids
     needing to maintain a permanent "demo project" registration just to
     keep the homepage populated. Generate the two demo files once (e.g.
     `static/demo-table.svg`, `static/demo-contributors.svg`) using
     realistic placeholder data (a handful of languages at varied
     percentages, a grid of placeholder avatar circles) and reference them
     directly with plain `<img>` tags.
   - Primary CTA button (amber): "Set up your badges" → `/setup`
   - Secondary link: "View on GitHub"

2. **How it works** (numbered — genuinely sequential here, so numbering is
   earned per the design guidance)
   1. Create a read-only, project-scoped Crowdin token
   2. Paste it once — we encrypt it immediately
   3. Copy two image URLs into your README
   - Each step: short label + one sentence, small inline SVG icon (key icon,
     lock icon, clipboard icon respectively)

3. **Security explanation** (this is a trust-critical page for a tool asking
   for API tokens — give it real estate, not a footnote)
   - Plain-language version of the `SECURITY.md` guarantee statement (§5)
   - Explicitly state: "We use project-scoped tokens and NaCl encryption.
     The encryption key never touches our database. See exactly how in the
     source code" → link to the GitHub repo
   - This section should look distinct from marketing copy — e.g. a
     bordered panel styled like a terminal/code block, reinforcing
     "this is a technical claim you can verify," not a trust badge/seal
     graphic

4. **Customization example**
   - Show the `?limit=` and `?unit=` query parameters on a real URL, with a
     small inline diagram of 2-3 variations (e.g. contributor grid at
     `limit=10` vs `limit=30`) so users immediately understand the
     endpoints are configurable before they even reach `/setup`

5. **FAQ**
   Rendered as a plain, non-collapsible list (not an accordion — this is
   short enough to just read, and an accordion hides content from
   search/Ctrl-F which matters for a security-adjacent FAQ). Minimum
   questions to cover:

   - **"How do I revoke access?"**
     Delete the Personal Access Token in your Crowdin account — this is the
     real kill switch and takes effect immediately, before the service's
     cache even expires. To also remove your project's row from our
     database, email `revoke@yourdomain.eu` with your badge URL (the
     `public_id` in the image URL is enough to identify it). We manually
     confirm and delete within [X days] — no automated self-service delete
     exists in v1, and this page says so plainly rather than implying one
     does.
   - **"What can the token you're storing actually do?"**
     Explains that if the user follows the Granular Access + read-only +
     single-project setup instructed during onboarding, a worst-case
     compromise of our database (ciphertext only, per §5) or of a live
     decrypted token in transit is limited to read-only access to that one
     project — it cannot modify translations, invite members, or access any
     other project. Explicitly note this is the *user's* responsibility to
     configure correctly; the token creation instructions default to it,
     but nothing on Crowdin's side enforces it if a user pastes a
     broader-scoped token instead.
   - **"What happens if I delete my Crowdin token but forget to email you?"**
     The badge endpoints will start failing (Crowdin returns 401) on the
     next cache refresh; the last successfully cached SVG keeps being served
     stale until it errors out, at which point the image simply stops
     updating/loading. The row stays in the database, encrypted and
     unusable, until a revoke email is sent. State plainly that this is a
     manual-cleanup limitation of v1, not a hidden auto-expiry.
   - **"Can you read my Crowdin data?"**
     Direct, short answer: the service can only see what your scoped
     token permits (translation progress and top-members reports on the
     one project you registered) — link to `SECURITY.md` for the technical
     detail rather than re-explaining encryption here. Note that the
     Crowdin Project ID itself (not the token) is stored unencrypted, since
     it isn't a credential — relevant mainly if your project is private;
     see `SECURITY.md` for the precise statement.
   - **"Is this affiliated with Crowdin?"**
     No — independent, unofficial, built against Crowdin's public API using
     their official Go client. Say this explicitly to avoid any implied
     endorsement.
   - **"What if I change my mind about a `limit` or `unit` value?"**
     Just edit the query string in the README — no need to re-onboard, the
     `public_id` doesn't change.
   - **"Where's the source code?"**
     Link directly to the GitHub repo — for a tool asking for API tokens,
     "you can read exactly what this does" is a stronger trust signal than
     any explanation on the page itself.

6. **Footer**
   - Link to GitHub repo, `SECURITY.md`, and revoke instructions
     (`mailto:revoke@yourdomain.eu`)
   - No newsletter signup, no social links beyond GitHub — keep it to what's
     actually useful to this audience

### 9.3 Onboarding form (`GET /setup`, `POST /setup`)

Reached via the landing page's primary CTA. Single card, centered, on the
same dark background/token system as the landing page — visually continuous,
not a jarring handoff to a different-looking "app."

**Flow:**
1. Short instructional panel above the form: create a Crowdin Personal
   Access Token, enable **Granular Access**, scope it to **this one project
   only**, read-only — with a direct link to Crowdin's token creation page.
2. Two fields, monospace input styling:
   - Crowdin Project ID
   - Personal Access Token (password-masked, monospace, with a show/hide
     toggle icon — a masked technical credential is hard to proofread, and
     letting the user verify what they pasted reduces failed submissions)
3. Primary button: "Generate badges" (amber, disabled state while
   submitting, label changes to "Verifying...")
4. Submit → `POST /setup` via `fetch`.
5. **On success**: form is replaced (not appended below) by a results panel:
   - Copyable code block (monospace) with the ready-to-paste markdown
   - Live `<img>` preview of the actual generated `table.svg`
   - A small note: "Save this page or the URLs above — there's no login, so
     this is the only place you'll see them."
6. **On error**: inline error message directly under the form, in the
   interface's own voice, specific about what went wrong (invalid token,
   wrong scope, rate limited) — never vague, never apologetic.
7. Form fields are cleared immediately after a successful submission so the
   token isn't left sitting in the DOM/browser autofill history longer than
   necessary.

**Revocation instructions**, shown in a small panel below the form:
> To revoke access, delete the Personal Access Token in your Crowdin account
> — this immediately stops the service from accessing your project. To also
> remove your data from our database, email `revoke@yourdomain.eu` with your
> badge URL.

**Security headers for `/setup` specifically (via Caddy):**
```
Referrer-Policy: no-referrer
Content-Security-Policy: default-src 'self'
```
Prevents token leakage via referrer headers and blocks third-party script
injection on the one page that handles a secret.

### 9.4 Implementation notes

- **Tailwind**: use the Tailwind CLI to compile a single static stylesheet at
  build time (`tailwindcss -i input.css -o /static/app.css --minify`), not
  the browser-runtime CDN script — keeps the CSP simple (`style-src 'self'`)
  and avoids a render-blocking external script on every page load.
- **Icons**: vendor a small hand-picked set of inline SVGs (key, lock,
  clipboard, check, eye/eye-off for the token toggle) directly into the HTML
  templates — no runtime fetch to an icon CDN.
- **Fonts**: self-host Inter and JetBrains Mono (woff2, subset to Latin) under
  `/static/fonts/` rather than pulling from Google Fonts — one less external
  request, keeps the CSP tight, and avoids a third-party seeing every visitor
  to a page that's about to collect an API token.
- Both `/` and `/setup` are plain, static `http.ServeFile` handlers — no
  templating engine needed. The landing page has no server-side data
  dependency at all, since its example imagery is the hardcoded demo SVGs
  described in §9.2, not a live-rendered call to the badge endpoints.
- Demo SVGs can reuse the same `renderSVGTable` / `renderContributorsSVG`
  functions from §11 at build time (a small one-off script or `go run` step
  that writes fixed sample data to `static/demo-table.svg` and
  `static/demo-contributors.svg`), so the landing page's example always
  stays visually identical to real output without any drift between the
  two.

---

## 10. Revocation

**v1 approach: email-based, no token-based revoke link.**

- **Primary kill switch**: the user deletes their Crowdin PAT themselves. This
  immediately breaks the service's ability to call Crowdin on their behalf —
  no action required on the operator's side for access to stop.
- **Secondary/cleanup**: user emails `revoke@yourdomain.eu` with their badge
  URL to request the database row be purged.
- **Manual action on receipt of a revoke email:**
  ```sql
  UPDATE projects SET revoked = 1 WHERE public_id = 'xxxx';
  -- or, for full deletion:
  DELETE FROM projects WHERE public_id = 'xxxx';
  DELETE FROM cache WHERE key LIKE 'table:xxxx%' OR key LIKE 'contrib:xxxx%';
  ```
- Both badge handlers must check `revoked = 0` in their project lookup query,
  so a revoked project 404s immediately rather than serving stale cache.

---

## 11. SVG rendering

### `table.svg`
Simple horizontal bar per language: label, background track, filled bar
scaled to progress %, percentage label.

### `contributors.svg`
Grid of circular avatars (contrib.rocks style), using `<clipPath>` circles +
`<image href="...">` per contributor — no canvas/raster library needed, pure
SVG string composition. Members ordered by contribution volume (per the
selected `unit`) before rendering, then truncated to `limit`.

Both renderers are pure functions: `[]LanguageProgress → string` and
`[]Member → string`. No dependency on request/response objects, easy to unit
test independently of HTTP handling.

---

## 12. Project structure

```
crowdin-stats/
├── main.go               # server setup, routing, startup
├── crypto.go               # encrypt/decrypt (§5)
├── crowdin.go                # wrapper around official client calls (§6)
├── render.go                  # SVG generation (§11)
├── cache.go                     # cache get/refresh logic, stale-while-revalidate
├── ratelimit.go                   # §8
├── db.go                            # SQLite connection, migrations
├── schema.sql                         # §4
├── static/
│   ├── index.html                       # landing page (§9.2)
│   ├── setup.html                        # onboarding form (§9.3)
│   ├── app.css                            # compiled Tailwind output (§9.4)
│   ├── demo-table.svg                       # hardcoded landing page example (§9.2)
│   ├── demo-contributors.svg                 # hardcoded landing page example (§9.2)
│   └── fonts/                              # self-hosted Inter + JetBrains Mono
├── tailwind.config.js
├── input.css                          # Tailwind source, compiled to static/app.css
├── Dockerfile
├── docker-compose.yml
├── Caddyfile
├── .env.example
└── SECURITY.md
```

---

## 13. docker-compose.yml

```yaml
services:
  app:
    build: .
    environment:
      - MASTER_KEY=${MASTER_KEY}
      - DB_PATH=/data/db.sqlite
      - HOST=badges.yourdomain.eu
    volumes:
      - ./data:/data
    restart: unless-stopped

  caddy:
    image: caddy:2
    ports:
      - "443:443"
      - "80:80"
    volumes:
      - ./Caddyfile:/etc/caddy/Caddyfile
      - caddy_data:/data
    depends_on:
      - app
    restart: unless-stopped

volumes:
  caddy_data:
```

```
# Caddyfile
badges.yourdomain.eu {
    header /setup {
        Referrer-Policy "no-referrer"
        Content-Security-Policy "default-src 'self'"
    }
    reverse_proxy app:8080
}
```

```dockerfile
# Dockerfile
FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /crowdin-stats .

FROM alpine:3.20
COPY --from=build /crowdin-stats /crowdin-stats
COPY static /static
ENTRYPOINT ["/crowdin-stats"]
```

`CGO_ENABLED=0` + `modernc.org/sqlite` (pure Go) gives a small static binary
with no C toolchain dependency, for reproducible builds.

```bash
# .env.example — copy to .env, generate a real key, chmod 600, never commit
MASTER_KEY=                    # generate with: openssl rand -base64 32
DB_PATH=/data/db.sqlite
HOST=badges.yourdomain.eu
```

---

## 14. Deployment checklist

1. `openssl rand -base64 32` → `.env` as `MASTER_KEY`; `chmod 600 .env`;
   confirm gitignored.
2. `docker compose up -d --build`.
3. Point DNS for `badges.yourdomain.eu` at the VM; Caddy handles TLS
   automatically.
4. End-to-end test `/setup` with a real project-scoped Crowdin token.
5. Manually inspect `db.sqlite` to confirm only ciphertext is present
   (sanity check, not a substitute for code review of the encryption path).
6. Set up automated backups per §15.1 (WAL-safe method — a raw file `cp` of
   a live SQLite database can produce a corrupt copy).
7. Publish `SECURITY.md` with the guarantee statement from §5.

---

## 15. Operations

### 16.1 Backups

**Do not `cp`/`rsync` the raw `db.sqlite` file while the app is running** —
with WAL mode enabled, a naive file copy mid-write can capture an
inconsistent snapshot. Use SQLite's own safe backup mechanism instead:

```bash
sqlite3 /data/db.sqlite "VACUUM INTO '/backups/db-$(date +%F).sqlite'"
```

Run via a daily cron/systemd timer on the host (outside the container, or
via `docker compose exec`), retain e.g. 14 days, and store copies off the
VM (encrypted-at-rest storage is fine, but not strictly required beyond
what's already true of the source file — all sensitive fields are
ciphertext).

**Restore procedure** (documented, not just backup): stop the `app`
container, replace `/data/db.sqlite` with the chosen backup file, restart.
Worth a short runbook note in the repo rather than leaving this
undocumented until the day it's actually needed.

### 16.2 Cache and rate-limit cleanup

Two tables grow unboundedly without a sweep:

```go
func startCleanupTicker(db *sql.DB) {
    ticker := time.NewTicker(time.Hour)
    go func() {
        for range ticker.C {
            now := time.Now().Unix()
            db.Exec(`DELETE FROM rate_limits WHERE window_start < ?`, now-7200)
            db.Exec(`DELETE FROM cache WHERE expires_at < ?`, now-86400) // 24h grace past TTL
        }
    }()
}
```

Cache rows get a grace period past their 12h TTL (rather than immediate
deletion at expiry) so a brief traffic gap doesn't force an unnecessary
cold-start fetch on the next request.

### 16.3 Schema migrations

`schema.sql` covers a fresh install. For changes to a running production
database, use a minimal migration tool rather than hand-editing — even
`golang-migrate` with a couple of numbered `.sql` files is enough to avoid
ad hoc drift between environments. Document the current schema version
alongside `schema.sql`.

### 16.4 Observability

Minimum viable logging (structured, e.g. `log/slog`):
- Per-request: method, path, status code, duration — **never** request or
  response bodies, and `/setup` excluded from any middleware that might
  capture them by default.
- Crowdin API call outcomes (success/failure/latency) — useful for noticing
  if Crowdin itself is degraded before users report broken badges.
- Rate-limit rejections — a spike here is usually the first sign of abuse.

Minimum viable monitoring: disk usage alert on the VM (SQLite file +
backups), and an uptime check against `/healthz` (§7). Neither requires
new infrastructure — a simple `cron` + `curl` + webhook, or a free-tier
external uptime monitor, is enough at this scale.

### 16.5 Data residency (EU hosting)

Since self-hosting on an EU VM was an explicit goal, state this plainly in
`SECURITY.md` rather than leaving it implicit: no third-party
subprocessors are involved in storing or processing tokens (no cloud KMS,
no managed database, no external cache) — the entire data path is the
operator's own VM plus Crowdin's API itself, which the user has already
chosen to trust by using Crowdin.

---

## 17. Explicitly deferred / out of scope for v1

- No user accounts or login system.
- No token-based self-service revoke link (email-based only, see §10).
- No GitHub-repo-based public contributor data — contributors are sourced
  exclusively from Crowdin's Top Members report (project translators/
  proofreaders), not GitHub commit history.
- No multi-region/HA deployment — single VM is the target.
- No admin dashboard — database inspected/edited directly via `sqlite3` CLI
  when needed (e.g. manual revocation).
- No automated `MASTER_KEY` rotation — documented manual limitation, see
  §5.4.
- ~~No SSRF-prone server-side avatar fetching~~ — **reversed post-v1.**
  `contributors.svg` originally embedded avatar URLs directly via
  `<image href>`, left for the viewer's browser to resolve client-side.
  Real-world testing showed this doesn't actually work: browsers refuse to
  load an external `<image href>` inside an SVG when that SVG is used as
  an `<img src>` — which is exactly how every badge is displayed in a
  README (confirmed against Firefox bug 628747, "SVG-as-an-image
  shouldn't be able to load external resources"). Opening the raw SVG URL
  directly worked fine, which made this easy to miss in manual testing.
  The fix (`avatar.go`): fetch each avatar server-side and inline it as a
  base64 `data:` URI at render time, matching how contrib.rocks and
  similar tools handle this. SSRF exposure is bounded, not eliminated:
  HTTPS-only, 5s timeout, 2MB body cap, `Content-Type` must start with
  `image/`, and any failure just falls back to the initials circle rather
  than erroring the whole badge. The avatar URL itself comes from
  Crowdin's own report response, not directly from user input.
