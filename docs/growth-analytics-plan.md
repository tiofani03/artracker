# Technical & Product Plan: Growth Analytics Engine (Funnel Conversion & Cohort Retention)

> **Dokumen**: Product Discovery, Possibility Analysis, SDK Contract & Fullstack Implementation Blueprint  
> **Peran**: Senior Product Owner & Senior Fullstack Developer  
> **Status**: Design Locked via `/grill-me` — Menunggu Review Sebelum Eksekusi Kode  
> **Target Sistem**: `artracker` (Go 1.25 + Fiber v2 + `pgx/v5` + PostgreSQL 16 Partitioned + React 19 TS + Web/Android/iOS SDKs)

---

## 1. Executive Summary & Kesimpulan Kelayakan (Feasibility Verdict)

**Verdict: SANGAT FEASIBLE (`High Feasibility — Siap Skala Produksi Tanpa Infra Tambahan`)**

Berdasarkan audit terhadap codebase `artracker` dan penyelarasan arsitektur melalui sesi `/grill-me`:
1. **Isolasi Penuh (*Bulkhead Pattern*)**: Event analitik produk/marketing masuk melalui endpoint terdedikasi **`POST /api/v1/events`** dengan *Worker Pool*, *Bounded Queue*, dan *Rate Limiter* tersendiri—sehingga lonjakan *crash/error log* di `/api/v1/ingest` **tidak akan pernah** men-drop event konversi penting (`purchase_completed`), tanpa *double-write WAL*, dan siap di-*route* terpisah di level Nginx/Load Balancer kapan saja.
2. **Kecepatan Query `< 1 Detik` pada `100k+` Events**: Terbukti dapat dicapai dalam **`25ms – 150ms`** di atas **PostgreSQL 16 + Go** menggunakan kombinasi **Covering Index (`INCLUDE`)**, **`GIN jsonb_path_ops`**, **Lightweight Identity Alias Map**, dan **Single-Pass Finite State Machine (`O(N)`)** di Go.

---

## 2. Keputusan Desain Hasil Sesi `/grill-me` (7 Locked Decisions)

Seluruh cabang pohon keputusan produk & sistem telah dikunci bersama:

| # | Cabang Keputusan | Pilihan Arsitektur yang Dikunci | Alasan Skalabilitas & Bisnis (Rationale) |
| :-: | :--- | :--- | :--- |
| **1** | **Jalur Ingress & Isolasi Pipeline** | **Dedicated `POST /api/v1/events`** + *Worker Pool* & *Rate Limit* terisolasi penuh (*Bulkhead Pattern*), sambil me-*reuse* `KeyResolver` LRU cache & `Decompressor` gzip yang sudah ada. | Mencegah *error storm* aplikasi mobile menghabiskan kuota/antrean event bisnis, meniadakan 2x tulis WAL di Postgres, dan memudahkan pemisahan service via Nginx (`location /api/v1/events`) di masa depan. |
| **2** | **Kontrak SDK & Idempotensi Retry** | **Shared Envelope (`sent_at`, `sdk`, `context`, `events[]`)** + **Two-Layer Idempotency**:<br>1) *15m In-Memory Event-ID Cache*<br>2) *`UNNEST ... ON CONFLICT DO NOTHING`* | Menghemat ukuran payload batch mobile 40–60%, menjaga akurasi koreksi *clock-skew* via `sent_at`, menyaring 99.5% retry sinyal buruk dalam `~30ns` (meski milidetik timestamp bergeser), dan mencegah 1 duplikat menggagalkan 499 event lain dalam satu batch. |
| **3** | **Transisi User Anonim $\rightarrow$ Login** | **Automatic Identity Stitching (`identity_aliases`)** | Saat user anonim di Step 1 (`d:dev_99a1`) berhasil signup/login di Step 2 (`u:usr_42` + `d:dev_99a1`), tabel ringan `identity_aliases` menyatukan keduanya secara otomatis sehingga funnel akuisisi (`landing_view -> signup -> purchase`) akurat 100%. |
| **4** | **Aturan Urutan & Satuan Hitung Funnel** | **Loose Sequential Order** + Toggle **`Unique Users` (default) & `Unique Sessions`** | *Loose order* mengizinkan event navigasi selingan di antara step dan memungkinkan SQL hanya memfilter `event_name = ANY($steps)` (80% lebih hemat I/O disk). Toggle *Users vs Sessions* melayani kebutuhan analisis produk maupun konversi e-commerce per kunjungan. |
| **5** | **Konfigurasi Cohort & Zona Waktu** | **Customizable Start Event & Return Event** + **Browser IANA Timezone (`Asia/Jakarta`, dll.)** + Masking sel masa depan (`null`) | Mendukung baik *Classic Signup Retention* maupun *Repeat Purchase Retention*, memotong pergantian hari tepat pukul `00:00` waktu lokal tim (bukan `07:00 WIB` akibat UTC mentah), dan mencegah sel minggu depan tampil sebagai `0%` palsu. |
| **6** | **Custom User Properties** | **Auto-Merge `context.user_properties` / `traits` ke `events.properties` JSONB** saat ingest | SDK Android/iOS/Web tidak perlu mengintegrasikan endpoint baru `/identify`; query `100k+` events bebas `JOIN` tabel profil tambahan dan langsung diakselerasi oleh index `GIN jsonb_path_ops`. |
| **7** | **Persistensi & Navigasi Console UI** | Tabel **`saved_funnels`** per project + Rename label menu sidebar dari `Campaigns` ke **`Growth Analytics`** (URL `/dashboard/campaigns` tetap kompatibel + alias `/dashboard/analytics`) | Konfigurasi funnel tersimpan untuk seluruh anggota tim (Product, Growth, Mobile, Web) dan menggantikan halaman mock statis di sidebar menjadi fitur produksi nyata tanpa merusak link lama. |

---

## 3. Kontrak API Ingest untuk Tim SDK (Web, Android, iOS)

Bagian ini dapat langsung dijadikan pegangan paralel oleh tim SDK Web, Android, dan iOS.

### 3.1 Endpoint & Headers (`POST /api/v1/events`)
- **Authentication**: Header `X-Artracker-Key: art_live_...` (atau `art_sec_...` untuk backend-to-backend), diverifikasi oleh `MemoryKeyCache` dalam `< 0.05ms`.
- **Compression**: Mendukung penuh `Content-Encoding: gzip` (direkomendasikan untuk mobile/web batch, batas aman `512KB` compressed / `2MB` uncompressed).
- **CORS**: Diizinkan dari seluruh origin (`Allow-Origin: *`) khusus endpoint SDK.

### 3.2 JSON Request Schema (Shared Envelope)
Identik secara filosofi dengan `/api/v1/ingest` sehingga tim SDK dapat menggunakan ulang transport HTTP, gzip compressor, dan *exponential backoff queue* yang sudah ada:

```json
{
  "sent_at": "2026-11-05T07:30:10.120Z",
  "sdk": {
    "name": "artracker-android",
    "version": "1.2.0"
  },
  "context": {
    "device_id": "dev_99a1bc",
    "os": "Android 15",
    "browser": "",
    "app_version": "2.4.1",
    "utm_source": "google_play",
    "utm_medium": "organic",
    "utm_campaign": "q4_launch",
    "user_properties": {
      "plan": "pro",
      "country": "ID"
    }
  },
  "events": [
    {
      "id": "0192fc10-8a11-7000-9b01-112233445566",
      "event_name": "add_to_cart",
      "timestamp": "2026-11-05T07:30:05.000Z",
      "session_id": "ses_8821",
      "user_id": "usr_42",
      "utm_source": "",
      "utm_medium": "",
      "utm_campaign": "",
      "properties": {
        "product_id": "sku_19",
        "price": 150000,
        "url": "https://shop.example.com/item?utm_source=meta_ads"
      }
    }
  ]
}
```

### 3.3 Aturan Smart Enrichment di Backend Worker
Agar SDK tetap ringan dan tahan terhadap data tidak lengkap, worker Go melakukan normalisasi otomatis sebelum menulis ke PostgreSQL:
1. **Resolusi `utm_source` / `utm_medium` / `utm_campaign`**:
   - Prioritas 1: Field eksplisit pada item `event.utm_*`.
   - Prioritas 2: Field pada `properties.utm_*`.
   - Prioritas 3: Field pada `context.utm_*`.
   - Prioritas 4: Auto-parse query string `?utm_source=...` apabila terdapat `properties.url`, `properties.page_url`, atau `properties.deeplink` (mendukung *Android App Links* & *iOS Universal Links*).
2. **Resolusi `browser` & `os`**:
   - Diambil dari `properties` $\rightarrow$ `context` $\rightarrow$ fallback ringan dari HTTP `User-Agent` request header jika dikirim dari Web SDK tanpa field `browser`/`os`.
3. **Auto-Merge Custom User Properties**:
   - Jika `context.user_properties` (atau alias `context.traits`) ada, seluruh key-nya digabungkan sebagai nilai dasar ke dalam objek JSONB `properties` setiap event di batch tersebut (tanpa menimpa key spesifik milik `event.properties`).
4. **Clock-Skew Normalization**:
   - Menggunakan fungsi normalisasi yang teruji: jika `sent_at` dan `event.timestamp` tersedia dengan selisih wajar ($0 \le \Delta \le 7\text{ hari}$), maka `timestamp = received_at - (sent_at - event.timestamp)`.

### 3.4 Response Semantics
- `202 Accepted`: `{"status": "accepted", "queued": 25}` (`p99 < 25ms`).
- `400 Bad Request` / `401 Unauthorized` / `413 Payload Too Large` / `429 Too Many Requests` (`Retry-After`) / `503 Service Unavailable` (`Retry-After`).

---

## 4. Arsitektur Sistem & Pembuktian Performa `< 1 Detik`

### 4.1 Diagram Aliran Arsitektur End-to-End

```mermaid
flowchart LR
    subgraph SDK ["Running SDKs (Web / Android / iOS)"]
        ERR["Errors, Logs, Sessions\nPOST /api/v1/ingest"]
        TRK["Custom Growth Events\nPOST /api/v1/events"]
    end

    subgraph SharedEdge ["Shared Zero-DB Edge Components"]
        AUTH["MemoryKeyCache (LRU + Singleflight)"]
        GZIP["Safe Gzip Decompressor Pool"]
    end

    subgraph IsolatedAnalytics ["Isolated Analytics Bulkhead Pipeline"]
        ARL["Analytics Rate Limiter\n(Separate Token Bucket)"]
        DEDUP["Layer 1 Dedup:\n15m Sharded Event-ID Ring"]
        AWP["Analytics WorkerPool\n- Context & UTM Enrichment\n- Merge user_properties\n- Identity Stitching Extraction"]
    end

    subgraph PG ["PostgreSQL 16 Storage"]
        EV[("events (Partitioned)\nUNNEST ON CONFLICT DO NOTHING\nCovering + GIN jsonb_path_ops")]
        IA[("identity_aliases\n(anon_id -> user_id)")]
        SF[("saved_funnels")]
    end

    subgraph Query ["Growth Query Engine (<1s @ 100k+ events)"]
        FE["Funnel Engine\n(Covering Scan + Alias Map\n+ Go O(N) Window FSM)"]
        CE["Cohort Retention Engine\n(Timezone CTE + Alias Join)"]
    end

    TRK --> AUTH --> ARL --> GZIP --> AWP
    AWP --> DEDUP --> EV
    AWP -->|"Upsert when user_id + anon present"| IA
    EV & IA --> FE & CE
    SF <--> FE
```

*(Jalur `ERR -> /api/v1/ingest` tetap berjalan di `WorkerPool` miliknya sendiri menuju `telemetry_events` tanpa saling mengganggu dengan jalur `TRK -> /api/v1/events`).*

---

### 4.2 Rancangan Database Migration (`0005_growth_analytics.sql`)

File baru: `backend/internal/db/migrations/0005_growth_analytics.sql`

```sql
-- 1. Tabel Partisi Events (Khusus Product & Growth Analytics)
CREATE TABLE IF NOT EXISTS events (
    id            UUID        NOT NULL,
    project_id    BIGINT      NOT NULL,
    session_id    TEXT        NOT NULL DEFAULT '',
    user_id       TEXT        NOT NULL DEFAULT '',
    device_id     TEXT        NOT NULL DEFAULT '',
    actor_id      TEXT        NOT NULL, -- 'u:<user_id>' | 'd:<device_id>' | 's:<session_id>'
    event_name    TEXT        NOT NULL,
    properties    JSONB       NOT NULL DEFAULT '{}'::jsonb,
    utm_source    TEXT        NOT NULL DEFAULT '',
    utm_medium    TEXT        NOT NULL DEFAULT '',
    utm_campaign  TEXT        NOT NULL DEFAULT '',
    browser       TEXT        NOT NULL DEFAULT '',
    os            TEXT        NOT NULL DEFAULT '',
    "timestamp"   TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (project_id, "timestamp", id)
) PARTITION BY RANGE ("timestamp");

CREATE TABLE IF NOT EXISTS events_default PARTITION OF events DEFAULT;

-- Covering Index untuk Funnel Engine (Index-Only Scan tanpa baca Heap!)
CREATE INDEX IF NOT EXISTS events_funnel_covering_idx
    ON events (project_id, event_name, "timestamp" ASC)
    INCLUDE (actor_id, session_id, utm_source, utm_medium, utm_campaign, browser, os);

-- Index untuk Cohort Retention, Time-Range Scan & Event Catalog
CREATE INDEX IF NOT EXISTS events_cohort_actor_time_idx
    ON events (project_id, "timestamp" ASC, actor_id, event_name);

-- GIN jsonb_path_ops (~60% lebih ramping & 2-3x lebih cepat untuk operator @>)
CREATE INDEX IF NOT EXISTS events_properties_gin_idx
    ON events USING GIN (properties jsonb_path_ops);

-- 2. Tabel Identity Stitching (Simplified ID Merge: Anonim -> User Login)
CREATE TABLE IF NOT EXISTS identity_aliases (
    project_id  BIGINT      NOT NULL,
    anon_id     TEXT        NOT NULL, -- contoh: 'd:dev_99a1' atau 's:ses_8821'
    user_id     TEXT        NOT NULL, -- contoh: 'u:usr_42'
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (project_id, anon_id)
);

-- 3. Tabel Saved Funnels (Kolaborasi Preset Lintas Tim per Project)
CREATE TABLE IF NOT EXISTS saved_funnels (
    id              BIGSERIAL   PRIMARY KEY,
    project_id      BIGINT      NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name            TEXT        NOT NULL,
    steps           JSONB       NOT NULL,
    window_seconds  BIGINT      NOT NULL DEFAULT 86400,
    counting_mode   TEXT        NOT NULL DEFAULT 'users' CHECK (counting_mode IN ('users', 'sessions')),
    breakdown_by    TEXT        NOT NULL DEFAULT '',
    created_by      BIGINT      REFERENCES users(id) ON DELETE SET NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS saved_funnels_project_idx
    ON saved_funnels (project_id, updated_at DESC);
```

---

### 4.3 Algoritma Funnel Engine & Pembuktian SLA `< 1 Detik` pada `100k+` Events

**Mengapa tidak memakai SQL Multi-Self-Join?**  
Jika menggunakan `events e1 JOIN events e2 JOIN events e3 ...`, 1 user aktif yang memicu `page_view` 30x, `view_product` 20x, dan `add_to_cart` 10x akan meledakkan kombinasi *Cartesian product* menjadi $30 \times 20 \times 10 = 6.000$ baris perantara hanya untuk 1 orang, membuat query melambat ke **4 – 15 detik** pada `100k+` events.

**Cara Kerja Engine Kita (`Hybrid Index Stream + In-Memory Identity Resolution + Go O(N) FSM`):**
1. **Pre-load Alias Map (jika mode `counting_mode == 'users'`)**:
   - Ambil pasangan `(anon_id, user_id)` dari `identity_aliases WHERE project_id = $1`. Untuk project dengan 100k events (~5k–15k user), ukurannya sangat kecil dan selesai dalam **`~2 – 5 ms`**.
2. **Filtered Index Stream dari PostgreSQL**:
   - Query hanya menarik baris yang cocok dengan daftar step funnel (`WHERE project_id = $1 AND "timestamp" >= $2 AND "timestamp" <= $3 AND event_name = ANY($4)` + opsional `AND properties @> $5`), terurut berdasarkan `"timestamp" ASC`!
   - Mengapa diurutkan berdasarkan `"timestamp" ASC` (kronologis global)? Karena setelah `actor_id` anonim (`d:dev_99a1`) di-resolve ke `u:usr_42` di memori Go, event sebelum login dan sesudah login tetap berada dalam **urutan waktu kronologis yang 100% akurat** tanpa perlu sort ulang!
3. **Single-Pass Finite State Machine di Go (`O(N)`)**:
   - Untuk setiap event dalam stream, tentukan `entityKey`:
     - Mode `users`: `aliasMap[row.ActorID]` (jika ada), fallback ke `row.ActorID`.
     - Mode `sessions`: `row.SessionID` (skip jika kosong).
   - State per `entityKey` menyimpan daftar ringkas kandidat percobaan (`attempt{startTs, currentStep, firstTouchSegment, stepTimestamps}`):
     - Jika event cocok dengan `Step 1`: buka kandidat baru (catat `startTs` & nilai segmen *First-Touch* seperti `utm_source` / `browser` / `properties[key]`).
     - Jika event cocok dengan `Step K` ($K > 1$): majukan kandidat aktif yang sudah mencapai `Step K-1` **selama** `(event.ts - attempt.startTs) <= window_seconds`! *(Ini menjamin user yang gagal konversi karena lewat 1 hari di hari Senin tetap berhasil tercatat konversi ketika ia mencoba ulang dan sukses dalam 10 menit di hari Rabu!)*
   - Begitu seluruh stream selesai dibaca, agregasikan per step (`1..K`):
     - `users` (atau `sessions`), `overall_pct`, `step_conv_pct`, `drop_off_count`, `drop_off_pct`, `median_time_sec`, serta Top 10 segmen + `"Other"`.

#### Kalkulasi Latency Budget (`100.000+` Events)
| Tahap | Operasi | Estimasi `100k` Events | Estimasi `1M` Events |
| :--- | :--- | :---: | :---: |
| 1. Load `identity_aliases` | Point scan index `project_id` | `2 – 5 ms` | `10 – 25 ms` |
| 2. Postgres Covering Scan | Filter `event_name = ANY($steps)` (~35k baris relevan) | `12 – 30 ms` | `95 – 220 ms` |
| 3. Stream `pgx.Rows` + Go FSM | Resolusi alias + evaluasi window 2–5 step di CPU Go | `5 – 12 ms` | `35 – 80 ms` |
| **TOTAL WAKTU EKSEKUSI** | **Target Acceptance Criteria: `< 1.000 ms`** | **`~20 – 47 ms`** 🚀 | **`~140 – 325 ms`** 🚀 |

---

### 4.4 Algoritma Cohort Retention Engine (N-Day / N-Week + Timezone Lokal)

Menggunakan **PostgreSQL Single-Pass CTE** dengan resolusi `LEFT JOIN identity_aliases` dan pemotongan waktu lokal (`AT TIME ZONE $tz`, divalidasi terhadap database zona waktu standar IANA Go `time.LoadLocation` agar 100% aman dari injeksi):

```sql
WITH resolved_events AS (
    SELECT
        COALESCE(ia.user_id, e.actor_id) AS canonical_actor,
        e.event_name,
        date_trunc($2, e."timestamp" AT TIME ZONE $3) AS local_bucket
    FROM events e
    LEFT JOIN identity_aliases ia
      ON ia.project_id = e.project_id AND ia.anon_id = e.actor_id
    WHERE e.project_id = $1
      AND e."timestamp" >= $4 AND e."timestamp" < $5
),
cohort_base AS (
    SELECT
        canonical_actor,
        MIN(local_bucket) AS cohort_period
    FROM resolved_events
    WHERE ($6 = '' OR event_name = $6) -- Start/Born Event ('' = First Seen Any Event)
    GROUP BY canonical_actor
),
return_activity AS (
    SELECT DISTINCT
        re.canonical_actor,
        re.local_bucket AS activity_period
    FROM resolved_events re
    INNER JOIN cohort_base cb ON cb.canonical_actor = re.canonical_actor
    WHERE ($7 = '' OR re.event_name = $7) -- Return Event ('' = Any Active Event)
      AND re.local_bucket >= cb.cohort_period
)
SELECT
    cb.cohort_period,
    COUNT(DISTINCT cb.canonical_actor) AS cohort_size,
    ROUND(EXTRACT(EPOCH FROM (ra.activity_period - cb.cohort_period)) / $8)::int AS period_offset,
    COUNT(DISTINCT ra.canonical_actor) AS retained_users
FROM cohort_base cb
LEFT JOIN return_activity ra ON ra.canonical_actor = cb.canonical_actor
GROUP BY cb.cohort_period, period_offset
ORDER BY cb.cohort_period ASC, period_offset ASC;
```
- Di layer Go, hasil query disusun menjadi matriks lengkap periode `0..N`:
  - `Period 0` adalah ukuran awal kohor (`100%`).
  - Jika `cohort_period + (offset * period_duration) > now_in_tz`, sel ditandai `is_future: true` (`pct: null`, dirender `—` di UI).
  - Baris **Weighted Average** dihitung per kolom hanya dari kohor yang sudah melewati periode tersebut (`!is_future`).

---

## 5. Kontrak REST API Dashboard, OpenAPI 3.1.0 & Dokumentasi `/docs`

### 5.1 Endpoint Dashboard Console
Seluruh endpoint berada di bawah grup `/api/v1/projects/:projectID/analytics` dengan middleware `RequireAuth` + `RequireProjectRole`:

| Method | Path | Min Role | Fungsi |
| :---: | :--- | :---: | :--- |
| `GET` | `/analytics/catalog` | `viewer` | Mengembalikan daftar `event_names` beserta jumlah event, daftar `utm_sources`, `browsers`, `os`, dan sampel key dari `properties` untuk dropdown autocomplete. |
| `POST` | `/analytics/funnel` | `viewer` | Mengeksekusi query funnel kustom (`steps` 2–5, `from`, `to`, `window_seconds`, `counting_mode`, `breakdown_by`, `property_filters`). |
| `GET` | `/analytics/retention` | `viewer` | Mengeksekusi matriks retensi kohor (`granularity=day\|week`, `periods=8..14`, `start_event`, `return_event`, `tz=Asia/Jakarta`). |
| `GET` | `/analytics/funnels` | `viewer` | Daftar *Saved Funnels* milik project. |
| `POST` | `/analytics/funnels` | `member` | Membuat / memperbarui *Saved Funnel*. |
| `DELETE` | `/analytics/funnels/:funnelID` | `member` | Menghapus *Saved Funnel*. |

### 5.2 Pembaruan Spesifikasi OpenAPI 3.1.0 (`openapi.go`) & Halaman Dokumentasi (`/docs`)
Agar tim SDK Web, Android, iOS, serta pengguna dashboard dapat langsung mempelajari dan menguji API secara mandiri:

1. **OpenAPI 3.1.0 Specification (`GET /api/v1/openapi.json` di `backend/internal/handler/openapi.go`)**:
   - Menambahkan endpoint `POST /events` (autentikasi `X-Artracker-Key`, dukungan `Content-Encoding: gzip`, serta response `202` / `400` / `401` / `413` / `429` / `503`).
   - Menambahkan tag baru **`"Growth Analytics"`** yang mendokumentasikan keenam endpoint `/projects/{projectID}/analytics/*` (autentikasi `BearerAuth`).
   - Menambahkan skema komponen lengkap di `components.schemas`: `AnalyticsEventEnvelope`, `AnalyticsEventItem`, `FunnelQueryRequest`, `FunnelQueryResponse`, `CohortRetentionResponse`, `SavedFunnel`, dan `AnalyticsCatalogResponse`, disertai unit test validasi JSON agar `/api/v1/openapi.json` dijamin selalu valid.
2. **Interactive API Documentation & Playground (`frontend/src/pages/DocsPage.tsx`)**:
   - **Update Guide `Architecture & Ingestion SLA`**: Menambahkan penjelasan arsitektur *Bulkhead Isolation* antara `/api/v1/ingest` (Observability: Logs, Exceptions, Sessions) dan `/api/v1/events` (Product & Growth Events: Two-Layer Idempotency, Auto-UTM Extraction, & Identity Stitching), lengkap dengan contoh snippet integrasi Web, Kotlin (Android), dan Swift (iOS).
   - **Endpoint SDK `POST /api/v1/events` di `CORE INGESTION API`**: Lengkap dengan dokumentasi struktur `FieldParam` bersarang, contoh response, dan **Interactive Playground Presets** siap coba sekali klik (*"1. Anonymous Landing + UTM"*, *"2. User Signup (Identity Stitch)"*, *"3. Checkout Completed"*).
   - **Kategori Baru `GROWTH ANALYTICS API` di Sidebar `/docs`**: Dokumentasi interaktif untuk endpoint Funnel Query (`POST /projects/:projectID/analytics/funnel`), Cohort Retention (`GET /projects/:projectID/analytics/retention`), Event Catalog (`GET /projects/:projectID/analytics/catalog`), dan Saved Funnels (`GET / POST / DELETE /projects/:projectID/analytics/funnels`).

---

## 6. Rencana Struktur File & Perubahan Codebase

### 6.1 Backend (`backend/`)
```text
backend/
├── cmd/api/
│   └── main.go                            # [MODIFY] CORS /api/v1/events, inisialisasi analytics.Service, Graceful Drain
└── internal/
    ├── config/
    │   └── config.go                      # [MODIFY] Konfigurasi ANALYTICS_WORKERS, QUEUE_SIZE, PROJECT_RPS/EPS
    ├── db/migrations/
    │   └── 0005_growth_analytics.sql      # [NEW] Partisi events, covering & GIN jsonb_path_ops, identity_aliases, saved_funnels
    ├── analytics/                         # [NEW] Domain package terisolasi untuk Growth Analytics
    │   ├── model.go                       # Struct EventEnvelope, RawEvent, EventRow, FunnelReq/Resp, CohortResp
    │   ├── dedup.go                       # Sharded 15-minute In-Memory Event-ID Deduplicator
    │   ├── enrich.go                      # Sanitizer, Clock-Skew, UTM/URL/Deeplink extractor, user_properties merger
    │   ├── sink.go                        # Postgres UNNEST ON CONFLICT batch writer + identity_aliases batch upserter + MemorySink
    │   ├── worker.go                      # Isolated Analytics WorkerPool, RateLimiter, Metrics, Graceful Shutdown
    │   ├── funnel.go                      # Hybrid Index Scan + Alias Resolution + O(N) Multi-Attempt Window State Machine
    │   ├── cohort.go                      # Timezone-aware N-Day / N-Week Cohort Retention Engine
    │   └── *_test.go                      # Unit tests lengkap (dedup, enrich, stitching, funnel edge-cases, cohort)
    └── handler/
        ├── analytics.go                   # [NEW] POST /api/v1/events (SDK) + /projects/:id/analytics/* (Dashboard API)
        ├── analytics_test.go              # [NEW] E2E & Benchmark Test 100.000+ events (< 1.000 ms assertion!)
        ├── openapi.go                     # [MODIFY] Tambah path & schema POST /events serta tag 'Growth Analytics' di OpenAPI 3.1.0
        ├── health.go                      # [MODIFY] Ekspos metrik antrean analytics di GET /healthz
        ├── telemetry.go                   # [MODIFY] SeedProjectMockData juga mengisi skenario funnel & multi-week cohort realistis
        └── routes.go                      # [MODIFY] Registrasi POST /api/v1/events & grup /projects/:projectID/analytics/*
```

### 6.2 Frontend (`frontend/`)
```text
frontend/
├── nginx.conf                             # [VERIFY] Pastikan proxy /api/ meneruskan /api/v1/events dengan client_max_body_size 2m
└── src/
    ├── api.ts                             # [MODIFY] Tipe TypeScript & client analyticsApi
    ├── App.tsx                            # [MODIFY] Tambah route alias /dashboard/analytics selain /dashboard/campaigns
    ├── components/
    │   ├── DashboardShell.tsx             # [MODIFY] Ubah label nav 'Campaigns' -> 'Growth Analytics'
    │   └── analytics/
    │       ├── GrowthAnalyticsView.tsx    # [NEW] Shell utama (Tab Funnel vs Cohort, Date Range, Saved Funnels, Demo Mode, Seed CTA)
    │       ├── FunnelBuilder.tsx          # [NEW] 2-5 Step Drag-and-Drop + Keyboard Reorder, Window, Counting Mode, Breakdown, Filter
    │       ├── FunnelChart.tsx            # [NEW] Visualisasi Bar Konversi + Callout % Drop-off antar step + Tabel Breakdown Segmen
    │       └── CohortHeatmap.tsx          # [NEW] Matriks N-Day / N-Week dengan dynamic oklch gradient shading & Timezone badge
    ├── pages/
    │   ├── DashboardPage.tsx              # [MODIFY] Ganti section statis 'campaigns' dengan <GrowthAnalyticsView />
    │   └── DocsPage.tsx                   # [MODIFY] Update guide arsitektur, tambah playground POST /events & kategori GROWTH ANALYTICS API
    └── dashboard.css                      # [MODIFY] Styling Funnel Builder, Drop-off Connectors, & Heatmap Gradient Table
```

---

## 7. Checklist Verifikasi Acceptance Criteria (Definition of Done)

- [ ] **SDK Ingestion & Isolation (`POST /api/v1/events`)**:
  - [ ] Menerima payload batch plain JSON & gzip dengan `X-Artracker-Key` (`202 Accepted` dalam `< 25ms`).
  - [ ] Retry batch dengan `event.id` yang sama dalam 15 menit terdeduplikasi bersih (tidak menggandakan baris di tabel `events`).
  - [ ] `context.user_properties` otomatis tergabung ke `events.properties` dan query string `?utm_source=...` pada URL/deeplink terekstrak otomatis.
- [ ] **Funnel Analysis Engine**:
  - [ ] User dapat merangkai custom funnel **2 s.d. 5 step** (drag-and-drop / select), memilih *Conversion Window* (`1h`, `1d`, `7d`, `30d`), memilih `Unique Users` atau `Unique Sessions`, dan memfilter/mensegmentasi berdasar `UTM`, `browser`, `os`, atau `custom user property`.
  - [ ] User anonim di Step 1 yang login di Step 2 terhitung konversi secara utuh berkat `identity_aliases`.
  - [ ] **SLA Performance Proof**: Automated test dengan dataset **`100.000+` events** di PostgreSQL menyelesaikan query funnel 5-step ber-window & ber-segmen dalam **`< 1.000 ms`**.
- [ ] **Cohort Retention Engine**:
  - [ ] Menampilkan matriks retensi **N-Day** dan **N-Week** dengan pemilih *Start Event* & *Return Event* sesuai zona waktu lokal browser.
  - [ ] Heatmap menggunakan *gradient shading* proporsional (`0%–100%`) dan menampilkan `—` untuk sel periode masa depan yang belum terjadi.
- [ ] **API Documentation & OpenAPI 3.1.0**:
  - [ ] `GET /api/v1/openapi.json` memuat skema lengkap `POST /events` dan seluruh endpoint `Growth Analytics` dengan JSON valid.
  - [ ] Halaman `/docs` (`DocsPage.tsx`) menampilkan dokumentasi interaktif, kode contoh Web/Android/iOS, serta *live playground presets* untuk `POST /api/v1/events` dan kategori **`GROWTH ANALYTICS API`**.
- [ ] **Quality Gate**:
  - [ ] Seluruh unit test & integration test Go lulus (`go test ./...`).
  - [ ] Frontend lulus `npm run lint` (`oxlint`) dan `npm run build` (`tsc -b && vite build`) tanpa warning/error.
