# Art Tracker

Monorepo starter: **Go (Fiber + pgx)** API, **Vite + React + TypeScript** frontend,
**PostgreSQL**, everything runnable locally or via Docker Compose.

```
artracker/
├── backend/            Go API
│   ├── cmd/api/        entrypoint (graceful shutdown, migrations on boot)
│   └── internal/
│       ├── config/     env config
│       ├── db/         pool + embedded SQL migrations
│       └── handler/    HTTP handlers (health, artworks CRUD)
├── frontend/           Vite + React + TS
├── docker-compose.yml       dev: postgres only
├── docker-compose.prod.yml  full stack (db + api + web behind nginx)
├── docker-compose.build.yml local override that builds instead of pulling
└── .github/workflows/       ci.yml (tests) + docker-publish.yml (Docker Hub)
```

## Local development

Prerequisites: Go 1.25+, Node 22+, Docker.

```bash
cp .env.example .env
make db-up      # postgres on localhost:5433
make api        # Go API on :8080 (runs migrations at boot)
make web        # Vite on :5173, proxies /api -> :8080
```

Open http://localhost:5173.

## API

| Method | Path                 | Notes                          |
| ------ | -------------------- | ------------------------------ |
| GET    | `/healthz`           | pings the database             |
| GET    | `/api/v1/artworks`   | `?limit=50&offset=0`, newest first |
| POST   | `/api/v1/artworks`   | `{ "title": "...", ... }`      |
| GET    | `/api/v1/artworks/:id` |                              |
| PATCH  | `/api/v1/artworks/:id` | partial update               |
| DELETE | `/api/v1/artworks/:id` | 204 or 404                   |

Migrations live in `backend/internal/db/migrations` and are embedded into the
binary; each file runs once, tracked in `schema_migrations`.

## Auth & multi-tenancy

Login is Google SSO only. Users belong to **organizations**, organizations own **projects**, projects own **API keys**.

### Setup

1. Create a Google OAuth client (Cloud Console > APIs & Services > Credentials > OAuth client ID, type Web)
   and set the authorized redirect URI to `{PUBLIC_URL}/auth/callback/google`
   (dev: `http://localhost:5173/auth/callback/google`).
2. Put the credentials in `.env`: `GOOGLE_CLIENT_ID/SECRET`, plus `JWT_SECRET` (long random string, required in production) and `PUBLIC_URL`.
   Without Google credentials the login route returns 404. `.env.example` ships dummy values.

Sessions: a 15-minute access token (JWT, kept in memory by the SPA) and a 30-day refresh token (httpOnly cookie, rotated on every use; reusing a rotated token revokes the whole session).

### Roles

| Action | owner | admin | member | viewer |
| --- | :-: | :-: | :-: | :-: |
| View org, projects, key list (prefix only) | yes | yes | yes | yes |
| Create project | yes | yes | no | no |
| Create / revoke / rotate API key | yes | yes | no | no |

Anyone can create an organization and becomes its `owner`. Non-members get `404` for an org's projects and keys.

### API keys

- `art_live_...` public client key (for SDKs), `art_sec_...` secret key (server-to-server).
- The full key is shown once at creation; only a SHA-256 hash is stored.
- Validate a key: `curl -H 'X-Artracker-Key: <key>' /api/v1/sdk/validate` (200 with project id, else 401).
  `/api/v1/sdk/validate-admin` accepts secret keys only (public key gets 403).

### Tests

Handler and migration tests need Postgres and are skipped when `TEST_DATABASE_URL` is unset (CI provides it). Use a separate database, never your dev one:

```bash
docker compose exec db psql -U artracker -d postgres -c "CREATE DATABASE artracker_test"
TEST_DATABASE_URL='postgres://artracker:artracker@localhost:5433/artracker_test?sslmode=disable' make test
```

**These tests delete all users, organisations and projects in whatever database the DSN names.**

## Alerts (Telegram)

Artracker can notify a Telegram chat when a project's errors need attention.

Two bot modes, set with `ALERTS_BOT_MODE`:

- `tenant` (default, the SaaS mode): each organisation connects its own bot (created with [@BotFather](https://t.me/BotFather)) in Settings. Bot tokens are stored encrypted with the master key.
- `shared` (self-hosting): one operator bot for the whole deployment, from `TELEGRAM_BOT_TOKEN` or `TELEGRAM_BOT_TOKEN_FILE`.

The master key is `ALERTS_MASTER_KEY` (standard base64 of exactly 32 bytes) or `ALERTS_MASTER_KEY_FILE` (a file holding it); generate one with `openssl rand -base64 32` and keep it safe, since losing it makes stored bot tokens unreadable. It is optional in development and in `shared` mode, but a key that is set and malformed stops startup. `ALERTS_MASTER_KEY_ID` (default `v1`) labels the key.

**Upgrading:** `ALERTS_BOT_MODE` defaults to `tenant`, and in production that mode refuses to start without `ALERTS_MASTER_KEY`. An existing deployment must set either `ALERTS_MASTER_KEY` or `ALERTS_BOT_MODE=shared` **before** deploying this version (`docker-compose.prod.yml` already forwards the variables through `env_file: .env`).

Steps:

1. In `shared` mode set `TELEGRAM_BOT_TOKEN`; in `tenant` mode connect a bot in Settings.
2. Run exactly **one** API instance as the alerts worker. It polls Telegram and evaluates alert rules. Set `ALERTS_WORKER=false` on every extra replica (the default is `true`).
3. In the dashboard open **Alerts** for a project and use the connect link to add the bot to a group or a private chat, then send a test message.

Alert kinds: **new issue** (a never-seen error group), **regression** (a resolved issue occurs again) and **issue spike** (an issue's event rate jumps well above its recent baseline). Without a token incidents are still recorded in the dashboard, but no Telegram message is sent.

Rules default to the environment `production` and match the environment string your SDK reports exactly (case-sensitive): an app reporting `prod` or `Production` gets no alerts from a `production` rule. Clear a rule's environment field to alert on every environment, or set it to your SDK's value. An issue is "new" the first time it is seen in any environment; one first seen in staging will not raise a new-issue alert when it later appears in production.

## Images & deploy

Same flow as portalsafekamu: images are built in CI and published to Docker
Hub, the VPS only pulls them.

```bash
# on every push to main (.github/workflows/docker-publish.yml)
tayooooo/artracker-api:latest   (and :<git-sha>)
tayooooo/artracker-web:latest   (and :<git-sha>)
```

Repo secrets required: `DOCKER_USERNAME`, `DOCKER_PASSWORD` (a Docker Hub
access token, not the account password).

### Deploy on the VPS

```bash
git clone https://github.com/tiofani03/artracker.git ~/artracker
cd ~/artracker
cp .env.example .env          # set POSTGRES_PASSWORD
docker compose -f docker-compose.prod.yml pull
docker compose -f docker-compose.prod.yml up -d
curl localhost:8088/healthz
```

Update after CI finishes:

```bash
cd ~/artracker
docker compose -f docker-compose.prod.yml pull
docker compose -f docker-compose.prod.yml up -d
docker image prune -f
```

Pin a build with `TAG=<sha>` in `.env`. Migrations are embedded in the API
binary and run at boot, so there is no separate migrate step.

The stack publishes only the web container (`WEB_PORT`, default 8088); nginx
serves the SPA and proxies `/api` and `/healthz` to the API. Put Caddy/nginx
in front for TLS:

```caddyfile
art.exlm.my.id {
    reverse_proxy localhost:8088
}
```

CI can deploy by itself: set repo variable `DEPLOY_ENABLED=true` and secrets
`VPS_HOST`, `VPS_USER`, `VPS_SSH_KEY`.

### Build locally instead of pulling

```bash
docker compose -f docker-compose.prod.yml -f docker-compose.build.yml up -d --build
```

## Notes

- `artworks` is a starter table — replace it with the real domain model once known.
- No auth yet; add it before exposing write endpoints to the internet.
