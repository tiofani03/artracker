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
