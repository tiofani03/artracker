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
└── .github/workflows/ci.yml
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

## Docker (full stack)

```bash
cp .env.example .env         # set POSTGRES_PASSWORD
docker compose -f docker-compose.prod.yml up -d --build
curl localhost:8088/healthz
```

The stack publishes only the web container (`WEB_PORT`, default 8088); nginx
serves the SPA and proxies `/api` and `/healthz` to the API. Put Caddy/nginx
in front for TLS:

```caddyfile
art.exlm.my.id {
    reverse_proxy localhost:8088
}
```

## Notes

- `artworks` is a starter table — replace it with the real domain model once known.
- No auth yet; add it before exposing write endpoints to the internet.
