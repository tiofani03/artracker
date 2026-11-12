# Art Tracker

**Art Tracker** is an app observability platform: crash and error tracking, session replay,
product analytics and Telegram alerts. **Go (Fiber + pgx)** API, **Vite + React + TypeScript**
dashboard, **PostgreSQL**; runnable locally or via Docker Compose.

```
artracker/
├── backend/            Go API
│   ├── cmd/api/        entrypoint (graceful shutdown, migrations on boot)
│   ├── cmd/seed/       fills every project in the database with demo data
│   └── internal/
│       ├── alerts/         alert rules, incidents, Telegram delivery
│       ├── analytics/      funnels, retention, event sink
│       ├── auth/           JWT, roles, API key generation
│       ├── config/         env config
│       ├── db/             pool + embedded SQL migrations
│       ├── handler/        HTTP handlers and routes
│       ├── ingest/         SDK ingest pipeline (queue, rate limits, sanitising)
│       ├── issues/         crash grouping, source maps, stack trace parsing
│       ├── storage/        replay chunk storage (R2 / local disk)
│       └── symbolication/  ProGuard / R8 and iOS dSYM symbolication
├── frontend/           Vite + React + TS dashboard (src/pages, components, replay, issues)
├── docker-compose.yml       dev: postgres only
├── docker-compose.prod.yml  full stack (db + api + web behind nginx)
├── docker-compose.build.yml local override that builds instead of pulling
└── .github/workflows/       ci.yml (tests) + docker-publish.yml (Docker Hub)
```

## What it does

- **Issues**: errors sent by the SDKs are grouped by fingerprint into issues with counts, first/last
  seen and open / closed / muted status. The issue page shows the deobfuscated stack trace (your own
  frames highlighted), breadcrumbs, and breakdowns by OS version, device and app version, plus the
  affected users and a 14-day trend. Stack traces are deobfuscated with uploaded source maps (web),
  ProGuard / R8 mappings (Android) or dSYMs (iOS).
- **Session replay**: DOM replay for web and screenshot-mode replay for mobile (switch per project
  in Settings), with a performance profile per session.
- **Analytics**: event log, funnels (including saved funnels) and retention.
- **Alerts**: per-project rules for new issues, regressions and issue spikes, delivered to Telegram
  (see [Alerts](#alerts-telegram)).
- **Team**: organizations with invites and roles (see [Roles](#roles)).
- **Demo mode**: with `APP_ENV=development`, `?demo=1` in the dashboard URL (or the `X-Demo-Mode: 1`
  header) previews the dashboard without signing in. It is ignored in every other environment.

## Local development

Prerequisites: Go 1.25+, Node 22+, Docker.

```bash
cp .env.example .env
make db-up      # postgres on localhost:5433
make api        # Go API on $PORT (default 8080; runs migrations at boot)
make web        # Vite on $WEB_DEV_PORT (default 5173), proxies /api to the API
```

The API reads `PORT` from the environment or from `.env` (`go run` loads `../.env`). Vite reads the same
`PORT` to find the API, so a `PORT=8081` in `.env` just works; set `VITE_API_TARGET` to point the proxy
at a different API URL. Open the dashboard at http://localhost:5173 (or your `WEB_DEV_PORT`).

## API

- **SDK endpoints**, authenticated with a public client key in the `X-Artracker-Key` header:
  `POST /api/v1/ingest` (errors, logs), `POST /api/v1/events` (analytics events),
  `GET /api/v1/replay/config`, `POST /api/v1/replay/chunk-url` and `POST /api/v1/replay/chunk-ack`
  (session replay), `GET /api/v1/sdk/validate` (check a key).
- **Dashboard API** under `/api/v1`, authenticated with a login (JWT bearer) token: auth, orgs, members and
  invites, projects, API keys, issues, sessions, analytics, alerts and release artifacts. Roles are listed
  [below](#roles).
- `GET /healthz` pings the database and reports the ingest and analytics queues (`503` when degraded).
- **Reference**: the SDK guide with payloads is the dashboard's `/docs` page, and the machine-readable
  OpenAPI spec is served at `GET /api/v1/openapi.json`.
- `/api/v1/artworks` is a leftover starter example that nothing in the dashboard uses.

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
| View org, projects, members, issues, sessions, analytics, alerts, key list (prefix only) | yes | yes | yes | yes |
| Triage issues (open / close / mute, assign) | yes | yes | yes | yes |
| Save and delete funnels | yes | yes | yes | no |
| Create project, create / revoke / rotate API keys | yes | yes | no | no |
| Invite and remove members, manage alert rules, channels and Telegram bot | yes | yes | no | no |
| Change replay settings, upload source maps / ProGuard mappings / dSYMs | yes | yes | no | no |
| Change a member's role | yes | no | no | no |

Anyone can create an organization and becomes its `owner`. Non-members get `404` for an org's projects and keys.

### API keys

- `art_live_...` public client key (for SDKs), `art_sec_...` secret key (server-to-server). Today the
  secret key is only accepted by `/api/v1/sdk/validate-admin`.
- The full key is shown once at creation; only a SHA-256 hash is stored.
- Validate a key: `curl -H 'X-Artracker-Key: <key>' /api/v1/sdk/validate` (200 with project id, else 401).
  `/api/v1/sdk/validate-admin` accepts secret keys only (public key gets 403).

### Known limitations

- Uploading source maps, ProGuard mappings and dSYMs
  (`POST /api/v1/projects/:id/releases/:version/{sourcemaps,proguard,dsyms}`) needs a dashboard login (JWT)
  token from an admin. A secret key (`art_sec_...`) is not accepted there, so a CI pipeline cannot upload
  them with a key yet.

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

Privacy in `tenant` mode: the platform only calls Telegram's `sendMessage` and `getMe` and never reads updates, but the operator holds the master key and so could, in principle, decrypt a stored token and use the bot, including reading its updates. Use a dedicated, alert-only bot and keep BotFather's privacy mode on. Telegram itself sees every message it delivers, and even the `minimal` detail level still sends the project name, environment and release.

The master key is `ALERTS_MASTER_KEY` (standard base64 of exactly 32 bytes) or `ALERTS_MASTER_KEY_FILE` (a file holding it); generate one with `openssl rand -base64 32` and keep it safe, since losing it makes stored bot tokens unreadable. It is optional in development and in `shared` mode, but a key that is set and malformed stops startup. `ALERTS_MASTER_KEY_ID` (default `v1`) labels the key.

**Upgrading:** `ALERTS_BOT_MODE` defaults to `tenant`, and in production that mode refuses to start without the master key. An existing deployment must set either `ALERTS_MASTER_KEY` or `ALERTS_BOT_MODE=shared` in the server's `.env` **before** deploying this version (`docker-compose.prod.yml` forwards it through `env_file: .env`). The shipped `docker-compose.prod.yml` mounts no secret file, so with it use `ALERTS_MASTER_KEY`; `ALERTS_MASTER_KEY_FILE` only works after you mount the key file into the `api` container yourself (a compose change). The deploy workflow checks this first: when `.env` sets none of `ALERTS_MASTER_KEY`, `ALERTS_MASTER_KEY_FILE` or `ALERTS_BOT_MODE=shared`, it fails with a clear error before pulling anything and leaves the running containers untouched.

Steps:

1. In `shared` mode set `TELEGRAM_BOT_TOKEN`; in `tenant` mode set `ALERTS_MASTER_KEY`.
2. Run exactly **one** API instance as the alerts worker. It evaluates alert rules and sends the messages (in `shared` mode it also polls Telegram for the link flow). Set `ALERTS_WORKER=false` on every extra replica (the default is `true`).
3. `tenant` mode: an organisation admin connects the bot in **Settings → Telegram Bot**, then opens **Alerts** for a project, adds the chat id (the page explains how to find it) and sends a test message. `shared` mode: open **Alerts** for a project and use the connect link to add the bot to a group or a private chat, then send a test message.

Alert kinds: **new issue** (a never-seen error group), **regression** (a resolved issue occurs again) and **issue spike** (an issue's event rate jumps well above its recent baseline). Without a token incidents are still recorded in the dashboard, but no Telegram message is sent.

Rules default to the environment `production` and match the environment string your SDK reports exactly (case-sensitive): an app reporting `prod` or `Production` gets no alerts from a `production` rule. Clear a rule's environment field to alert on every environment, or set it to your SDK's value. An issue is "new" the first time it is seen in any environment; one first seen in staging will not raise a new-issue alert when it later appears in production.

**Troubleshooting**

1. Bots show as connected but alerts are not delivered after a deploy: check that `ALERTS_MASTER_KEY` is the same value as before. A wrong or rotated key cannot decrypt stored tokens; the dispatcher keeps the incidents pending and logs `cannot decrypt org bot token`. It does not mark the bots invalid.
2. The process refuses to start in production: set `ALERTS_MASTER_KEY` or `ALERTS_BOT_MODE=shared`.
3. A bot shows `invalid`: Telegram rejected its token (revoked or regenerated in BotFather). Paste the new token in Settings.
4. Incidents older than 24 hours with no working bot are skipped; the dashboard shows them as "not sent (no channel, or no working bot for 24 h)" and the exact reason is only in the log.

## Images & deploy

Same flow as portalsafekamu: images are built in CI and published to Docker
Hub, the VPS only pulls them.

```bash
# on every push to main, once the repo variable PUBLISH_IMAGES=true is set
# (.github/workflows/docker-publish.yml; publishing is off until then)
tayooooo/artracker-api:latest   (and :<git-sha>)
tayooooo/artracker-web:latest   (and :<git-sha>)
```

Repo secrets required: `DOCKER_USERNAME`, `DOCKER_PASSWORD` (a Docker Hub
access token, not the account password).

### Deploy on the VPS

```bash
git clone https://github.com/tiofani03/artracker.git ~/artracker
cd ~/artracker
cp .env.example .env          # then set the production values listed below
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

Production `.env` must set (the API refuses to start or compose refuses to run without them):

- `POSTGRES_PASSWORD`
- `JWT_SECRET`: a random string of at least 32 characters (the placeholder from `.env.example` is rejected)
- `PUBLIC_URL`: the dashboard's public URL (used for OAuth redirects and links in alerts)
- `GOOGLE_CLIENT_ID` / `GOOGLE_CLIENT_SECRET` to sign in
- `ALERTS_MASTER_KEY`, or `ALERTS_BOT_MODE=shared` (see [Alerts](#alerts-telegram))
- the `R2_*` variables, or `REPLAY_LOCAL_DIR`, if you want session replay

Pin a build with `TAG=<sha>` in `.env`. Migrations are embedded in the API
binary and run at boot, so there is no separate migrate step.

The stack publishes the web container (`WEB_PORT`, default 8088) and Postgres (`DB_PORT`, default 5432;
firewall it or remove the `ports` entry if the database must stay private); nginx
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

- Tests that need Postgres run only against a database you name in `TEST_DATABASE_URL` (see [Tests](#tests)).
