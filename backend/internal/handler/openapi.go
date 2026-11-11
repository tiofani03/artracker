package handler

import (
	_ "embed"

	"github.com/gofiber/fiber/v2"
)

// OpenAPISpec returns the OpenAPI 3.1.0 specification for the ARTracker Ingestion & Telemetry API.
func (h *Handler) OpenAPISpec(c *fiber.Ctx) error {
	c.Set("Content-Type", "application/json; charset=utf-8")
	return c.Send(openAPISpecBytes)
}

var openAPISpecBytes = []byte(`{
  "openapi": "3.1.0",
  "info": {
    "title": "ARTracker Telemetry & Ingestion API",
    "version": "1.0.0",
    "description": "High-performance real-time telemetry ingestion and observability platform engineered for sub-25ms ingestion SLA and 1M+ DAU scale."
  },
  "servers": [
    { "url": "/api/v1", "description": "Current Environment API" },
    { "url": "https://art.exlm.my.id/api/v1", "description": "Production Edge Cluster" }
  ],
  "paths": {
    "/ingest": {
      "post": {
        "summary": "Batch Telemetry Ingestion",
        "description": "Asynchronous high-throughput batch ingestion endpoint accepting up to 500 events, error logs, and session heartbeats. Responds in <25ms (HTTP 202 Accepted). Supports Content-Encoding: gzip.",
        "operationId": "ingestBatch",
        "tags": ["Ingestion"],
        "parameters": [
          {
            "name": "X-Artracker-Key",
            "in": "header",
            "required": true,
            "description": "Project client SDK key starting with art_live_",
            "schema": { "type": "string", "example": "art_live_a892b109c3d4e5f6" }
          },
          {
            "name": "Content-Encoding",
            "in": "header",
            "required": false,
            "description": "Optional compression encoding",
            "schema": { "type": "string", "enum": ["gzip"] }
          }
        ],
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": { "$ref": "#/components/schemas/IngestEnvelope" }
            }
          }
        },
        "responses": {
          "202": {
            "description": "Batch successfully enqueued for persistence",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "status": { "type": "string", "example": "accepted" },
                    "queued_items": { "type": "integer", "example": 2 }
                  }
                }
              }
            }
          },
          "400": { "description": "Invalid JSON or empty items array" },
          "401": { "description": "Missing or invalid client key" },
          "413": { "description": "Payload exceeds 512KB gzip or 2MB uncompressed limit" },
          "429": { "description": "Per-project token bucket rate limit exceeded" },
          "503": { "description": "Ingest buffer saturated" }
        }
      }
    },
    "/events": {
      "post": {
        "summary": "Batch Growth & Product Events Ingestion",
        "description": "Asynchronous high-throughput ingestion endpoint for custom product & marketing events. Supports Content-Encoding: gzip, two-layer idempotency deduplication, auto-UTM extraction, and user properties merge.",
        "operationId": "ingestEventsBatch",
        "tags": ["Ingestion"],
        "parameters": [
          {
            "name": "X-Artracker-Key",
            "in": "header",
            "required": true,
            "description": "Project client SDK key starting with art_live_",
            "schema": { "type": "string", "example": "art_live_a892b109c3d4e5f6" }
          },
          {
            "name": "Content-Encoding",
            "in": "header",
            "required": false,
            "description": "Optional compression encoding",
            "schema": { "type": "string", "enum": ["gzip"] }
          }
        ],
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": { "$ref": "#/components/schemas/AnalyticsEventEnvelope" }
            }
          }
        },
        "responses": {
          "202": {
            "description": "Events batch enqueued for persistence",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "status": { "type": "string", "example": "accepted" },
                    "queued_items": { "type": "integer", "example": 3 }
                  }
                }
              }
            }
          },
          "400": { "description": "Invalid JSON or empty events array" },
          "401": { "description": "Missing or invalid client key" },
          "413": { "description": "Payload exceeds 512KB gzip or 2MB uncompressed limit" },
          "429": { "description": "Rate limit exceeded" },
          "503": { "description": "Analytics buffer saturated" }
        }
      }
    },
    "/sdk/validate": {
      "get": {
        "summary": "Validate SDK Client Key",
        "description": "Low-latency key validation endpoint querying the in-memory LRU key cache.",
        "operationId": "validateKey",
        "tags": ["Ingestion"],
        "parameters": [
          {
            "name": "X-Artracker-Key",
            "in": "header",
            "required": true,
            "schema": { "type": "string" }
          }
        ],
        "responses": {
          "200": {
            "description": "Key is valid",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "valid": { "type": "boolean", "example": true },
                    "project_id": { "type": "integer", "example": 10 }
                  }
                }
              }
            }
          },
          "401": { "description": "Key is invalid or revoked" }
        }
      }
    },
    "/projects/{projectID}/stats": {
      "get": {
        "summary": "Get Project Telemetry Statistics",
        "description": "Aggregates telemetry sessions, event volume, crash-free session percentage, and hourly buckets from PostgreSQL.",
        "operationId": "getProjectStats",
        "tags": ["Telemetry Query"],
        "security": [{ "BearerAuth": [] }],
        "parameters": [
          { "name": "projectID", "in": "path", "required": true, "schema": { "type": "integer" } },
          { "name": "range", "in": "query", "required": false, "schema": { "type": "string", "enum": ["24h", "7d", "30d"], "default": "24h" } }
        ],
        "responses": {
          "200": {
            "description": "Telemetry statistics",
            "content": {
              "application/json": {
                "schema": { "$ref": "#/components/schemas/ProjectStats" }
              }
            }
          }
        }
      }
    },
    "/projects/{projectID}/events": {
      "get": {
        "summary": "List Telemetry Events",
        "description": "Streams chronologically ordered telemetry events, logs, and sessions directly from the PostgreSQL partitioned table.",
        "operationId": "listProjectEvents",
        "tags": ["Telemetry Query"],
        "security": [{ "BearerAuth": [] }],
        "parameters": [
          { "name": "projectID", "in": "path", "required": true, "schema": { "type": "integer" } },
          { "name": "kind", "in": "query", "required": false, "schema": { "type": "string", "enum": ["all", "event", "log", "session"], "default": "all" } },
          { "name": "limit", "in": "query", "required": false, "schema": { "type": "integer", "default": 50 } }
        ],
        "responses": {
          "200": { "description": "List of telemetry events" }
        }
      }
    },
    "/projects/{projectID}/test-event": {
      "post": {
        "summary": "Trigger Real Test Event",
        "description": "Enqueues a test telemetry item into the live worker queue and flushes it to PostgreSQL.",
        "operationId": "sendTestEvent",
        "tags": ["Telemetry Query"],
        "security": [{ "BearerAuth": [] }],
        "parameters": [
          { "name": "projectID", "in": "path", "required": true, "schema": { "type": "integer" } }
        ],
        "responses": {
          "200": { "description": "Event enqueued successfully" }
        }
      }
    },
    "/replay/chunk-url": {
      "post": {
        "summary": "Request Presigned Upload URL for Replay Chunk",
        "description": "Generates a 5-minute presigned PUT URL directly to Cloudflare R2 / S3 storage with locked Content-Length & gzip Content-Type.",
        "operationId": "getReplayChunkURL",
        "tags": ["Session Replay"],
        "security": [{ "ApiKeyAuth": [] }],
        "parameters": [
          {
            "name": "X-Artracker-Key",
            "in": "header",
            "required": true,
            "description": "Project client SDK key",
            "schema": { "type": "string" }
          }
        ],
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": { "$ref": "#/components/schemas/ReplayChunkURLRequest" }
            }
          }
        },
        "responses": {
          "200": {
            "description": "Presigned PUT URL generated successfully",
            "content": {
              "application/json": {
                "schema": { "$ref": "#/components/schemas/ReplayChunkURLResponse" }
              }
            }
          },
          "400": { "description": "Invalid session_id, chunk_seq, or size_bytes" },
          "401": { "description": "Missing or invalid api key" },
          "403": { "description": "Screenshot replay is switched off for this project: body {\"error\":\"screenshot_disabled\"} when format is artracker-screen-v1. SDKs must stop screenshot capture and fall back to wireframe mode." },
          "413": { "description": "Chunk size exceeds maximum limit (5MB)" },
          "429": { "description": "Replay rate limit exceeded (separate per-project bucket, REPLAY_RATE_RPS); honour Retry-After" },
          "503": { "description": "Object storage not configured" }
        }
      }
    },
    "/replay/config": {
      "get": {
        "summary": "Replay Capture Settings for the SDK",
        "description": "Tells a mobile SDK whether screenshot-mode replay (artracker-screen-v1) may be captured and uploaded for the key's project, and the frame-rate ceiling. SDKs call it when replay starts and cache the answer.",
        "operationId": "getReplayConfig",
        "tags": ["Session Replay"],
        "security": [{ "ApiKeyAuth": [] }],
        "parameters": [
          {
            "name": "X-Artracker-Key",
            "in": "header",
            "required": true,
            "description": "Project client SDK key",
            "schema": { "type": "string" }
          }
        ],
        "responses": {
          "200": {
            "description": "Capture settings",
            "content": {
              "application/json": {
                "schema": { "$ref": "#/components/schemas/ReplayConfigResponse" }
              }
            }
          },
          "401": { "description": "Missing or invalid api key" },
          "429": { "description": "Replay rate limit exceeded" }
        }
      }
    },
    "/projects/{projectID}/replay-settings": {
      "patch": {
        "summary": "Update Project Replay Settings",
        "description": "Admin-only. Switches screenshot-mode replay uploads on or off for the project; takes effect for every app version without a release.",
        "operationId": "updateProjectReplaySettings",
        "tags": ["Session Replay"],
        "security": [{ "BearerAuth": [] }],
        "parameters": [
          { "name": "projectID", "in": "path", "required": true, "schema": { "type": "integer" } }
        ],
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["screenshot_enabled"],
                "properties": { "screenshot_enabled": { "type": "boolean" } }
              }
            }
          }
        },
        "responses": {
          "200": { "description": "The updated project, including replay_screenshot_enabled" },
          "400": { "description": "screenshot_enabled is required" },
          "403": { "description": "Requires the admin role" }
        }
      }
    },
    "/projects/{projectID}/sessions/{sessionID}/chunks": {
      "get": {
        "summary": "Refresh Signed URLs for Replay Chunks",
        "description": "Returns freshly signed download URLs for a range of a session's chunks (at most 100), so the player can keep loading a long session after the URLs from the session detail expired.",
        "operationId": "listSessionChunks",
        "tags": ["Session Replay"],
        "security": [{ "BearerAuth": [] }],
        "parameters": [
          { "name": "projectID", "in": "path", "required": true, "schema": { "type": "integer" } },
          { "name": "sessionID", "in": "path", "required": true, "schema": { "type": "string" } },
          { "name": "from_seq", "in": "query", "required": false, "schema": { "type": "integer", "minimum": 0, "default": 0 } },
          { "name": "to_seq", "in": "query", "required": false, "schema": { "type": "integer", "minimum": 0 } }
        ],
        "responses": {
          "200": { "description": "Chunks in ascending chunk_seq order, each with a signed url" },
          "400": { "description": "Invalid session id, or from_seq / to_seq not a non-negative integer or from_seq > to_seq" }
        }
      }
    },
    "/projects/{projectID}/sessions/{sessionID}/perf": {
      "get": {
        "summary": "Session Profiling Samples",
        "description": "Merged CPU / memory / FPS / jank samples SDKs reported for the session (type 'perf' ingest items), sorted by time and deduplicated. Above 1200 samples the series is downsampled keeping the worst value per bucket, so spikes survive. A session without profiling data returns empty arrays.",
        "operationId": "getSessionPerf",
        "tags": ["Session Replay"],
        "security": [{ "BearerAuth": [] }],
        "parameters": [
          { "name": "projectID", "in": "path", "required": true, "schema": { "type": "integer" } },
          { "name": "sessionID", "in": "path", "required": true, "schema": { "type": "string" } }
        ],
        "responses": {
          "200": { "description": "{ interval_ms, device{ram_mb,storage_mb,os_version,cores,refresh_hz}, metrics[], samples[{t,cpu,mem_mb,fps,jank,frozen,threads,battery,thermal,ram_free_mb,storage_free_mb,net}], downsampled }" },
          "400": { "description": "Invalid session id" }
        }
      }
    },
    "/replay/chunk-ack": {
      "post": {
        "summary": "Acknowledge Replay Chunk Upload",
        "description": "Idempotently records uploaded chunk into PostgreSQL and aggregates session duration, error flags, and chunk count.",
        "operationId": "acknowledgeReplayChunk",
        "tags": ["Session Replay"],
        "security": [{ "ApiKeyAuth": [] }],
        "parameters": [
          {
            "name": "X-Artracker-Key",
            "in": "header",
            "required": true,
            "description": "Project client SDK key",
            "schema": { "type": "string" }
          }
        ],
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": { "$ref": "#/components/schemas/ReplayChunkAckRequest" }
            }
          }
        },
        "responses": {
          "200": {
            "description": "Chunk acknowledged and aggregated",
            "content": {
              "application/json": {
                "schema": { "$ref": "#/components/schemas/ReplayChunkAckResponse" }
              }
            }
          },
          "400": { "description": "Invalid chunk parameters" },
          "401": { "description": "Missing or invalid api key" }
        }
      }
    },
    "/projects/{projectID}/sessions": {
      "get": {
        "summary": "List Project Replay Sessions",
        "description": "Retrieves recorded browser sessions with filters for errors, rage clicks, user ID, environment, and time range.",
        "operationId": "listProjectSessions",
        "tags": ["Session Replay"],
        "security": [{ "BearerAuth": [] }],
        "parameters": [
          { "name": "projectID", "in": "path", "required": true, "schema": { "type": "integer" } },
          { "name": "limit", "in": "query", "required": false, "schema": { "type": "integer", "default": 25, "maximum": 100 } },
          { "name": "offset", "in": "query", "required": false, "schema": { "type": "integer", "default": 0 } },
          { "name": "has_error", "in": "query", "required": false, "schema": { "type": "boolean" } },
          { "name": "has_rage_click", "in": "query", "required": false, "schema": { "type": "boolean" } },
          { "name": "has_dead_click", "in": "query", "required": false, "schema": { "type": "boolean" } },
          { "name": "user_id", "in": "query", "required": false, "schema": { "type": "string" } },
          { "name": "user_prefix", "in": "query", "required": false, "description": "Case-insensitive prefix of the user id (max 128 chars); % and _ are literal. user_id stays an exact match.", "schema": { "type": "string", "maxLength": 128 } },
          { "name": "environment", "in": "query", "required": false, "schema": { "type": "string" } },
          { "name": "from", "in": "query", "required": false, "schema": { "type": "string", "format": "date-time" } },
          { "name": "to", "in": "query", "required": false, "schema": { "type": "string", "format": "date-time" } }
        ],
        "responses": {
          "200": {
            "description": "List of recorded sessions",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "items": { "type": "array", "items": { "$ref": "#/components/schemas/ReplaySessionItem" } },
                    "total": { "type": "integer", "example": 42 },
                    "limit": { "type": "integer", "example": 25 },
                    "offset": { "type": "integer", "example": 0 }
                  }
                }
              }
            }
          }
        }
      }
    },
    "/projects/{projectID}/sessions/{sessionID}": {
      "get": {
        "summary": "Get Replay Session with Chunks and Timeline Events",
        "description": "Returns session metadata, ordered chunk list with presigned GET download URLs (15-min TTL), and correlated telemetry errors.",
        "operationId": "getProjectSession",
        "tags": ["Session Replay"],
        "security": [{ "BearerAuth": [] }],
        "parameters": [
          { "name": "projectID", "in": "path", "required": true, "schema": { "type": "integer" } },
          { "name": "sessionID", "in": "path", "required": true, "schema": { "type": "string" } }
        ],
        "responses": {
          "200": {
            "description": "Replay session details and presigned player URLs",
            "content": {
              "application/json": {
                "schema": { "$ref": "#/components/schemas/ReplaySessionDetail" }
              }
            }
          },
          "404": { "description": "Session not found" }
        }
      }
    },
    "/projects/{projectID}/analytics/catalog": {
      "get": {
        "summary": "Get Event and Attribute Catalog",
        "description": "Returns distinct event names with frequency counts, captured UTM sources, browsers, OSes, and top property keys.",
        "operationId": "getAnalyticsCatalog",
        "tags": ["Growth Analytics"],
        "security": [{ "BearerAuth": [] }],
        "parameters": [
          { "name": "projectID", "in": "path", "required": true, "schema": { "type": "integer" } }
        ],
        "responses": {
          "200": {
            "description": "Catalog of tracking names and dimensions",
            "content": {
              "application/json": {
                "schema": { "$ref": "#/components/schemas/AnalyticsCatalogResponse" }
              }
            }
          }
        }
      }
    },
    "/projects/{projectID}/analytics/funnel": {
      "post": {
        "summary": "Calculate Funnel Conversion",
        "description": "Computes drop-off conversion rates, window duration constraints, and optional breakdown segments across 2 to 5 ordered steps.",
        "operationId": "calculateFunnel",
        "tags": ["Growth Analytics"],
        "security": [{ "BearerAuth": [] }],
        "parameters": [
          { "name": "projectID", "in": "path", "required": true, "schema": { "type": "integer" } }
        ],
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": { "$ref": "#/components/schemas/FunnelQueryRequest" }
            }
          }
        },
        "responses": {
          "200": {
            "description": "Funnel conversion analysis result",
            "content": {
              "application/json": {
                "schema": { "$ref": "#/components/schemas/FunnelQueryResponse" }
              }
            }
          },
          "400": { "description": "Invalid steps or date range" }
        }
      }
    },
    "/projects/{projectID}/analytics/retention": {
      "get": {
        "summary": "Calculate Cohort Retention Matrix",
        "description": "Computes an N-day or N-week cohort retention matrix with customizable start/return events and local timezone boundaries.",
        "operationId": "calculateCohortRetention",
        "tags": ["Growth Analytics"],
        "security": [{ "BearerAuth": [] }],
        "parameters": [
          { "name": "projectID", "in": "path", "required": true, "schema": { "type": "integer" } },
          { "name": "granularity", "in": "query", "required": false, "schema": { "type": "string", "enum": ["day", "week"], "default": "week" } },
          { "name": "periods", "in": "query", "required": false, "schema": { "type": "integer", "default": 8 } },
          { "name": "start_event", "in": "query", "required": false, "schema": { "type": "string" } },
          { "name": "return_event", "in": "query", "required": false, "schema": { "type": "string" } },
          { "name": "tz", "in": "query", "required": false, "schema": { "type": "string", "default": "UTC" } }
        ],
        "responses": {
          "200": {
            "description": "Cohort retention matrix",
            "content": {
              "application/json": {
                "schema": { "$ref": "#/components/schemas/CohortRetentionResponse" }
              }
            }
          }
        }
      }
    },
    "/projects/{projectID}/analytics/funnels": {
      "get": {
        "summary": "List Saved Funnel Presets",
        "description": "Retrieves all saved funnel definitions for the project.",
        "operationId": "listSavedFunnels",
        "tags": ["Growth Analytics"],
        "security": [{ "BearerAuth": [] }],
        "parameters": [
          { "name": "projectID", "in": "path", "required": true, "schema": { "type": "integer" } }
        ],
        "responses": {
          "200": {
            "description": "List of saved funnels",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "items": { "type": "array", "items": { "$ref": "#/components/schemas/SavedFunnel" } }
                  }
                }
              }
            }
          }
        }
      },
      "post": {
        "summary": "Save a Funnel Preset",
        "description": "Saves a 2-5 step custom funnel definition for team collaboration.",
        "operationId": "createSavedFunnel",
        "tags": ["Growth Analytics"],
        "security": [{ "BearerAuth": [] }],
        "parameters": [
          { "name": "projectID", "in": "path", "required": true, "schema": { "type": "integer" } }
        ],
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": { "$ref": "#/components/schemas/SaveFunnelInput" }
            }
          }
        },
        "responses": {
          "201": {
            "description": "Funnel definition saved",
            "content": {
              "application/json": {
                "schema": { "$ref": "#/components/schemas/SavedFunnel" }
              }
            }
          }
        }
      }
    },
    "/projects/{projectID}/analytics/funnels/{funnelID}": {
      "delete": {
        "summary": "Delete a Saved Funnel",
        "description": "Deletes a previously saved funnel preset.",
        "operationId": "deleteSavedFunnel",
        "tags": ["Growth Analytics"],
        "security": [{ "BearerAuth": [] }],
        "parameters": [
          { "name": "projectID", "in": "path", "required": true, "schema": { "type": "integer" } },
          { "name": "funnelID", "in": "path", "required": true, "schema": { "type": "integer" } }
        ],
        "responses": {
          "204": { "description": "Funnel deleted" }
        }
      }
    },
    "/projects/{projectID}/releases/{version}/dsyms": {
      "post": {
        "summary": "Upload iOS dSYM Binary or Zip Archive",
        "description": "Uploads iOS Mach-O debug symbols (dSYM bundle zip or raw DWARF binary) for server-side crash symbolication.",
        "operationId": "uploadDSYM",
        "tags": ["Crash Diagnostics"],
        "security": [{ "BearerAuth": [] }],
        "parameters": [
          { "name": "projectID", "in": "path", "required": true, "schema": { "type": "integer" } },
          { "name": "version", "in": "path", "required": true, "schema": { "type": "string" } },
          { "name": "file", "in": "query", "required": false, "schema": { "type": "string" } }
        ],
        "responses": {
          "201": { "description": "dSYM artifact stored" },
          "400": { "description": "Invalid dSYM archive or missing version" },
          "401": { "description": "Unauthorized" }
        }
      }
    },
    "/projects/{projectID}/releases/{version}/proguard": {
      "post": {
        "summary": "Upload Android ProGuard / R8 Mapping File",
        "description": "Uploads mapping.txt for de-obfuscating Android Kotlin/Java stack traces.",
        "operationId": "uploadProguard",
        "tags": ["Crash Diagnostics"],
        "security": [{ "BearerAuth": [] }],
        "parameters": [
          { "name": "projectID", "in": "path", "required": true, "schema": { "type": "integer" } },
          { "name": "version", "in": "path", "required": true, "schema": { "type": "string" } },
          { "name": "file", "in": "query", "required": false, "schema": { "type": "string" } }
        ],
        "responses": {
          "201": { "description": "ProGuard mapping stored" },
          "400": { "description": "Invalid ProGuard mapping syntax" },
          "401": { "description": "Unauthorized" }
        }
      }
    },
    "/projects/{projectID}/releases/{version}/artifacts": {
      "get": {
        "summary": "List Release Debug Artifacts",
        "description": "Lists all dSYMs, ProGuard mappings, and Source Maps uploaded for a specific project release version.",
        "operationId": "listReleaseArtifacts",
        "tags": ["Crash Diagnostics"],
        "security": [{ "BearerAuth": [] }],
        "parameters": [
          { "name": "projectID", "in": "path", "required": true, "schema": { "type": "integer" } },
          { "name": "version", "in": "path", "required": true, "schema": { "type": "string" } }
        ],
        "responses": {
          "200": { "description": "List of artifacts for the release" }
        }
      }
    },
    "/projects/{projectID}/alerts/rules": {
      "get": {
        "summary": "List Alert Rules",
        "operationId": "listAlertRules",
        "tags": ["Alerts"],
        "security": [{ "BearerAuth": [] }],
        "parameters": [
          { "name": "projectID", "in": "path", "required": true, "schema": { "type": "integer" } }
        ],
        "responses": {
          "200": { "description": "{items: Rule[]} ordered new_issue, regression, issue_spike" }
        }
      }
    },
    "/projects/{projectID}/alerts/rules/{kind}": {
      "patch": {
        "summary": "Update an Alert Rule (admin)",
        "operationId": "updateAlertRule",
        "tags": ["Alerts"],
        "security": [{ "BearerAuth": [] }],
        "parameters": [
          { "name": "projectID", "in": "path", "required": true, "schema": { "type": "integer" } },
          { "name": "kind", "in": "path", "required": true, "schema": { "type": "string", "enum": ["new_issue", "regression", "issue_spike"] } }
        ],
        "responses": {
          "200": { "description": "The updated rule" },
          "400": { "description": "Invalid value, empty body, or spike field on a non-spike rule" },
          "404": { "description": "Unknown rule kind" }
        }
      }
    },
    "/projects/{projectID}/alerts/channels": {
      "get": {
        "summary": "List Alert Channels",
        "operationId": "listAlertChannels",
        "tags": ["Alerts"],
        "security": [{ "BearerAuth": [] }],
        "parameters": [
          { "name": "projectID", "in": "path", "required": true, "schema": { "type": "integer" } }
        ],
        "responses": {
          "200": { "description": "{telegram: {configured, bot_username}, mode: tenant|shared, bot: {configured, bot_username, status, status_reason, detail_level, updated_at, mode} (the project's organisation; configured false in shared mode), items: Channel[]}. Each channel has origin (linked|manual), destination (the chat of a manual channel, empty for linked) and active (linked channels are active in shared mode, manual ones in tenant mode); a linked channel's chat id is never returned" }
        }
      },
      "post": {
        "summary": "Add a Manual Chat Channel (admin, tenant mode)",
        "description": "Adds a chat delivered through the organisation's own bot. chat is a numeric chat id (^-?[1-9][0-9]{4,19}$) or a public @username (^@[A-Za-z][A-Za-z0-9_]{4,31}$); label is 1 to 80 Unicode code points, not only whitespace, on one line with no control characters. Telegram is not contacted: the channel is saved with chat_type empty (the platform only ever calls sendMessage and getMe).",
        "operationId": "createAlertChannel",
        "tags": ["Alerts"],
        "security": [{ "BearerAuth": [] }],
        "parameters": [
          { "name": "projectID", "in": "path", "required": true, "schema": { "type": "integer" } }
        ],
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["chat", "label"],
                "additionalProperties": false,
                "properties": {
                  "chat": { "type": "string" },
                  "label": { "type": "string", "minLength": 1, "maxLength": 80 }
                }
              }
            }
          }
        },
        "responses": {
          "201": { "description": "The channel (origin manual, chat_type empty)" },
          "400": { "description": "invalid JSON body, invalid chat, invalid label, or manual channels are only available in tenant mode" },
          "409": { "description": "connect a Telegram bot first, or chat already added" }
        }
      }
    },
    "/projects/{projectID}/alerts/channels/link-code": {
      "post": {
        "summary": "Create a Telegram Link Code (admin)",
        "operationId": "createAlertLinkCode",
        "tags": ["Alerts"],
        "security": [{ "BearerAuth": [] }],
        "parameters": [
          { "name": "projectID", "in": "path", "required": true, "schema": { "type": "integer" } }
        ],
        "responses": {
          "201": { "description": "Link code with group_link and private_link, valid 15 minutes" },
          "404": { "description": "link flow is disabled in tenant mode" },
          "503": { "description": "Telegram is not configured on this server" }
        }
      }
    },
    "/projects/{projectID}/alerts/channels/{channelID}": {
      "patch": {
        "summary": "Enable or Disable a Channel (admin)",
        "operationId": "updateAlertChannel",
        "tags": ["Alerts"],
        "security": [{ "BearerAuth": [] }],
        "parameters": [
          { "name": "projectID", "in": "path", "required": true, "schema": { "type": "integer" } },
          { "name": "channelID", "in": "path", "required": true, "schema": { "type": "integer" } }
        ],
        "responses": {
          "200": { "description": "The updated channel; enabling clears disabled_reason" },
          "404": { "description": "Channel not found" }
        }
      },
      "delete": {
        "summary": "Delete a Channel (admin)",
        "operationId": "deleteAlertChannel",
        "tags": ["Alerts"],
        "security": [{ "BearerAuth": [] }],
        "parameters": [
          { "name": "projectID", "in": "path", "required": true, "schema": { "type": "integer" } },
          { "name": "channelID", "in": "path", "required": true, "schema": { "type": "integer" } }
        ],
        "responses": {
          "204": { "description": "Channel deleted" },
          "404": { "description": "Channel not found" }
        }
      }
    },
    "/projects/{projectID}/alerts/channels/{channelID}/test": {
      "post": {
        "summary": "Send a Test Message (admin)",
        "operationId": "testAlertChannel",
        "tags": ["Alerts"],
        "security": [{ "BearerAuth": [] }],
        "parameters": [
          { "name": "projectID", "in": "path", "required": true, "schema": { "type": "integer" } },
          { "name": "channelID", "in": "path", "required": true, "schema": { "type": "integer" } }
        ],
        "responses": {
          "204": { "description": "Test message sent (tenant mode: through the organisation's bot, at its detail level)" },
          "404": { "description": "Channel not found, or not active in this bot mode" },
          "409": { "description": "connect a Telegram bot first (tenant mode, no working bot)" },
          "502": { "description": "Telegram rejected the test message" },
          "503": { "description": "Telegram is not configured on this server" }
        }
      }
    },
    "/orgs/{orgID}/alerts/telegram-bot": {
      "get": {
        "summary": "Get the Organisation's Telegram Bot",
        "description": "Any organisation member. Never returns the token. In shared mode configured is false and mode is shared.",
        "operationId": "getOrgTelegramBot",
        "tags": ["Alerts"],
        "security": [{ "BearerAuth": [] }],
        "parameters": [
          { "name": "orgID", "in": "path", "required": true, "schema": { "type": "integer" } }
        ],
        "responses": {
          "200": { "description": "{configured, bot_username, status (active|invalid|empty), status_reason, detail_level (minimal|full), updated_at, mode (tenant|shared)}" },
          "404": { "description": "Organization not found (or not a member)" }
        }
      },
      "put": {
        "summary": "Connect or Replace the Organisation's Telegram Bot (admin)",
        "description": "Validates the token with getMe and stores it encrypted. The token can never be read back. detail_level is optional (a new bot defaults to minimal; a replaced bot keeps its level).",
        "operationId": "putOrgTelegramBot",
        "tags": ["Alerts"],
        "security": [{ "BearerAuth": [] }],
        "parameters": [
          { "name": "orgID", "in": "path", "required": true, "schema": { "type": "integer" } }
        ],
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["token"],
                "additionalProperties": false,
                "properties": {
                  "token": { "type": "string", "maxLength": 256, "writeOnly": true },
                  "detail_level": { "type": "string", "enum": ["minimal", "full"] }
                }
              }
            }
          }
        },
        "responses": {
          "200": { "description": "The bot as GET shows it" },
          "400": { "description": "invalid JSON body, invalid bot token, or invalid detail level" },
          "403": { "description": "Insufficient role" },
          "404": { "description": "tenant bots are disabled in this deployment" },
          "502": { "description": "could not reach Telegram" },
          "503": { "description": "alerts master key is not configured" }
        }
      },
      "patch": {
        "summary": "Change the Bot's Detail Level (admin)",
        "operationId": "patchOrgTelegramBot",
        "tags": ["Alerts"],
        "security": [{ "BearerAuth": [] }],
        "parameters": [
          { "name": "orgID", "in": "path", "required": true, "schema": { "type": "integer" } }
        ],
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["detail_level"],
                "additionalProperties": false,
                "properties": {
                  "detail_level": { "type": "string", "enum": ["minimal", "full"] }
                }
              }
            }
          }
        },
        "responses": {
          "200": { "description": "The bot as GET shows it" },
          "400": { "description": "invalid JSON body or invalid detail level" },
          "404": { "description": "no bot configured, or tenant bots are disabled in this deployment" }
        }
      },
      "delete": {
        "summary": "Remove the Organisation's Telegram Bot (admin)",
        "description": "Idempotent. The organisation's manual channels are paused until a bot is connected again.",
        "operationId": "deleteOrgTelegramBot",
        "tags": ["Alerts"],
        "security": [{ "BearerAuth": [] }],
        "parameters": [
          { "name": "orgID", "in": "path", "required": true, "schema": { "type": "integer" } }
        ],
        "responses": {
          "204": { "description": "Bot removed (or there was none)" },
          "404": { "description": "tenant bots are disabled in this deployment" }
        }
      }
    },
    "/projects/{projectID}/alerts/incidents": {
      "get": {
        "summary": "List Alert Incidents",
        "operationId": "listAlertIncidents",
        "tags": ["Alerts"],
        "security": [{ "BearerAuth": [] }],
        "parameters": [
          { "name": "projectID", "in": "path", "required": true, "schema": { "type": "integer" } },
          { "name": "status", "in": "query", "required": false, "schema": { "type": "string", "enum": ["firing", "resolved", "all"], "default": "all" } },
          { "name": "limit", "in": "query", "required": false, "schema": { "type": "integer", "default": 50, "minimum": 1, "maximum": 200 } }
        ],
        "responses": {
          "200": { "description": "{items: Incident[]} newest first" },
          "400": { "description": "Invalid status filter" }
        }
      }
    }
  },
  "components": {
    "securitySchemes": {
      "BearerAuth": {
        "type": "http",
        "scheme": "bearer",
        "bearerFormat": "JWT"
      },
      "ApiKeyAuth": {
        "type": "apiKey",
        "in": "header",
        "name": "X-Artracker-Key"
      }
    },
    "schemas": {
      "IngestEnvelope": {
        "type": "object",
        "required": ["items"],
        "properties": {
          "sent_at": { "type": "string", "format": "date-time", "description": "Device timestamp when envelope was dispatched" },
          "sdk": {
            "type": "object",
            "properties": {
              "name": { "type": "string", "example": "artracker-browser" },
              "version": { "type": "string", "example": "1.2.0" }
            }
          },
          "context": {
            "type": "object",
            "description": "Global environment context",
            "properties": {
              "environment": { "type": "string", "example": "production" },
              "app_version": { "type": "string", "example": "2.4.1" }
            }
          },
          "items": {
            "type": "array",
            "minItems": 1,
            "maxItems": 500,
            "items": { "$ref": "#/components/schemas/RawItem" }
          }
        }
      },
      "RawItem": {
        "type": "object",
        "required": ["type"],
        "properties": {
          "type": { "type": "string", "enum": ["event", "log", "session"] },
          "name": { "type": "string", "maxLength": 255, "example": "checkout_completed" },
          "level": { "type": "string", "enum": ["debug", "info", "warn", "error", "fatal"] },
          "message": { "type": "string", "maxLength": 4096 },
          "session_id": { "type": "string", "maxLength": 128, "example": "ses_94829" },
          "user_id": { "type": "string", "maxLength": 128, "example": "usr_83910" },
          "timestamp": { "type": "string", "format": "date-time" },
          "attributes": {
            "type": "object",
            "description": "Flexible JSON attributes dictionary (max 32KB)",
            "example": { "utm_source": "google", "utm_campaign": "utm_launch_q4", "revenue": 1250000 }
          }
        }
      },
      "ProjectStats": {
        "type": "object",
        "properties": {
          "range": { "type": "string", "example": "24h" },
          "sessions_count": { "type": "integer", "example": 148 },
          "events_count": { "type": "integer", "example": 892 },
          "errors_count": { "type": "integer", "example": 4 },
          "error_free_pct": { "type": "string", "example": "97.3%" },
          "error_note": { "type": "string", "example": "4 unhandled" },
          "buckets": {
            "type": "array",
            "items": {
              "type": "object",
              "properties": {
                "label": { "type": "string", "example": "00:00" },
                "sessions": { "type": "integer", "example": 24 },
                "errors": { "type": "integer", "example": 0 }
              }
            }
          }
        }
      },
      "ReplayChunkURLRequest": {
        "type": "object",
        "required": ["session_id", "chunk_seq", "size_bytes"],
        "properties": {
          "session_id": { "type": "string", "example": "sess_01j8xyz9abc123" },
          "chunk_seq": { "type": "integer", "minimum": 0, "example": 0 },
          "size_bytes": { "type": "integer", "maximum": 5242880, "example": 45000 },
          "user_id": { "type": "string", "example": "usr_42" },
          "environment": { "type": "string", "example": "production" },
          "start_time": { "type": "string", "format": "date-time" },
          "format": {
            "type": "string",
            "description": "Chunk format about to be uploaded. Send artracker-screen-v1 for screenshot-mode mobile replay; omit for rrweb / wireframe chunks.",
            "example": "artracker-screen-v1"
          }
        }
      },
      "ReplayConfigResponse": {
        "type": "object",
        "properties": {
          "screenshot_enabled": { "type": "boolean", "description": "Server-side switch: whether screenshot-mode replay may be uploaded for this project", "example": true },
          "max_fps": { "type": "integer", "description": "Frame-rate ceiling for screenshot mode", "example": 10 }
        }
      },
      "ReplayChunkURLResponse": {
        "type": "object",
        "properties": {
          "upload_url": { "type": "string", "example": "https://artracker-storage.r2.cloudflarestorage.com/projects/1/sessions/sess_.../chunk_0.gz?X-Amz-Signature=..." },
          "object_key": { "type": "string", "example": "projects/1/sessions/sess_01j8xyz9abc123/chunk_0.gz" },
          "expires_in": { "type": "integer", "example": 300 }
        }
      },
      "ReplayChunkAckRequest": {
        "type": "object",
        "required": ["session_id", "chunk_seq", "size_bytes"],
        "properties": {
          "session_id": { "type": "string", "example": "sess_01j8xyz9abc123" },
          "chunk_seq": { "type": "integer", "minimum": 0, "example": 0 },
          "size_bytes": { "type": "integer", "example": 45000 },
          "duration_ms": { "type": "integer", "example": 15000 },
          "event_count": { "type": "integer", "example": 128 },
          "has_error": { "type": "boolean", "example": true },
          "has_rage_click": { "type": "boolean", "example": false },
          "has_dead_click": { "type": "boolean", "example": false }
        }
      },
      "ReplayChunkAckResponse": {
        "type": "object",
        "properties": {
          "status": { "type": "string", "example": "acknowledged" },
          "session_id": { "type": "string", "example": "sess_01j8xyz9abc123" },
          "chunk_seq": { "type": "integer", "example": 0 },
          "chunk_count": { "type": "integer", "example": 1 }
        }
      },
      "ReplaySessionItem": {
        "type": "object",
        "properties": {
          "id": { "type": "string", "example": "sess_01j8xyz9abc123" },
          "project_id": { "type": "integer", "example": 1 },
          "user_id": { "type": "string", "example": "usr_42" },
          "environment": { "type": "string", "example": "production" },
          "start_time": { "type": "string", "format": "date-time" },
          "end_time": { "type": "string", "format": "date-time" },
          "duration_ms": { "type": "integer", "example": 45000 },
          "chunk_count": { "type": "integer", "example": 3 },
          "event_count": { "type": "integer", "example": 384 },
          "total_size_bytes": { "type": "integer", "example": 135000 },
          "has_error": { "type": "boolean", "example": true },
          "has_rage_click": { "type": "boolean", "example": false },
          "has_dead_click": { "type": "boolean", "example": false },
          "storage_prefix": { "type": "string", "example": "projects/1/sessions/sess_01j8xyz9abc123/" },
          "created_at": { "type": "string", "format": "date-time" },
          "updated_at": { "type": "string", "format": "date-time" }
        }
      },
      "ReplayChunkDetail": {
        "type": "object",
        "properties": {
          "chunk_seq": { "type": "integer", "example": 0 },
          "object_key": { "type": "string", "example": "projects/1/sessions/sess_.../chunk_0.gz" },
          "size_bytes": { "type": "integer", "example": 45000 },
          "event_count": { "type": "integer", "example": 128 },
          "created_at": { "type": "string", "format": "date-time" },
          "url": { "type": "string", "example": "https://artracker-storage.r2.cloudflarestorage.com/projects/1/sessions/.../chunk_0.gz?X-Amz-Signature=..." }
        }
      },
      "ReplaySessionDetail": {
        "type": "object",
        "properties": {
          "session": { "$ref": "#/components/schemas/ReplaySessionItem" },
          "chunks": {
            "type": "array",
            "items": { "$ref": "#/components/schemas/ReplayChunkDetail" }
          },
          "timeline_events": {
            "type": "array",
            "items": {
              "type": "object",
              "properties": {
                "id": { "type": "string" },
                "kind": { "type": "string", "example": "log" },
                "name": { "type": "string", "example": "TypeError: Cannot read properties of undefined" },
                "level": { "type": "string", "example": "error" },
                "message": { "type": "string" },
                "event_timestamp": { "type": "string", "format": "date-time" }
              }
            }
          }
        }
      },
      "AnalyticsEventItem": {
        "type": "object",
        "required": ["event_name"],
        "properties": {
          "id": { "type": "string", "format": "uuid" },
          "event_name": { "type": "string", "example": "add_to_cart" },
          "timestamp": { "type": "string", "format": "date-time" },
          "session_id": { "type": "string", "example": "ses_94829" },
          "user_id": { "type": "string", "example": "usr_83910" },
          "device_id": { "type": "string", "example": "dev_abc123" },
          "utm_source": { "type": "string", "example": "google" },
          "utm_medium": { "type": "string", "example": "cpc" },
          "utm_campaign": { "type": "string", "example": "q4_sale" },
          "browser": { "type": "string", "example": "Chrome" },
          "os": { "type": "string", "example": "Android" },
          "properties": { "type": "object", "example": { "product_id": "sku_99", "price": 150000 } }
        }
      },
      "AnalyticsEventEnvelope": {
        "type": "object",
        "required": ["events"],
        "properties": {
          "sent_at": { "type": "string", "format": "date-time" },
          "sdk": {
            "type": "object",
            "properties": {
              "name": { "type": "string", "example": "artracker-android" },
              "version": { "type": "string", "example": "1.2.0" }
            }
          },
          "context": {
            "type": "object",
            "properties": {
              "device_id": { "type": "string", "example": "dev_abc123" },
              "os": { "type": "string", "example": "Android 15" },
              "app_version": { "type": "string", "example": "2.4.1" },
              "user_properties": { "type": "object", "example": { "plan": "pro" } }
            }
          },
          "events": {
            "type": "array",
            "items": { "$ref": "#/components/schemas/AnalyticsEventItem" }
          }
        }
      },
      "FunnelQueryRequest": {
        "type": "object",
        "required": ["steps"],
        "properties": {
          "steps": { "type": "array", "items": { "type": "string" }, "example": ["landing_view", "view_product", "add_to_cart", "purchase_completed"] },
          "from": { "type": "string", "format": "date-time" },
          "to": { "type": "string", "format": "date-time" },
          "window_seconds": { "type": "integer", "example": 86400, "default": 86400 },
          "counting_mode": { "type": "string", "enum": ["users", "sessions"], "default": "users" },
          "breakdown_by": { "type": "string", "example": "utm_source" },
          "property_filters": { "type": "object", "example": { "country": "ID" } }
        }
      },
      "FunnelStepResult": {
        "type": "object",
        "properties": {
          "step": { "type": "integer", "example": 1 },
          "event_name": { "type": "string", "example": "landing_view" },
          "count": { "type": "integer", "example": 10500 },
          "overall_pct": { "type": "number", "example": 100.0 },
          "step_conv_pct": { "type": "number", "example": 100.0 },
          "drop_off_count": { "type": "integer", "example": 0 },
          "drop_off_pct": { "type": "number", "example": 0.0 },
          "median_seconds": { "type": "number", "example": 0.0 }
        }
      },
      "FunnelBreakdown": {
        "type": "object",
        "properties": {
          "segment": { "type": "string", "example": "google" },
          "overall_conversion_pct": { "type": "number", "example": 24.5 },
          "step_counts": { "type": "array", "items": { "type": "integer" }, "example": [5000, 3200, 1800, 1225] }
        }
      },
      "FunnelQueryResponse": {
        "type": "object",
        "properties": {
          "execution_ms": { "type": "integer", "example": 38 },
          "window_seconds": { "type": "integer", "example": 86400 },
          "counting_mode": { "type": "string", "example": "users" },
          "overall_conversion_pct": { "type": "number", "example": 18.5 },
          "steps": { "type": "array", "items": { "$ref": "#/components/schemas/FunnelStepResult" } },
          "breakdowns": { "type": "array", "items": { "$ref": "#/components/schemas/FunnelBreakdown" } }
        }
      },
      "CohortRetentionResponse": {
        "type": "object",
        "properties": {
          "granularity": { "type": "string", "example": "week" },
          "timezone": { "type": "string", "example": "Asia/Jakarta" },
          "start_event": { "type": "string", "example": "signup" },
          "return_event": { "type": "string", "example": "" },
          "cohorts": {
            "type": "array",
            "items": {
              "type": "object",
              "properties": {
                "cohort_label": { "type": "string", "example": "Wk of 06 Oct" },
                "cohort_date": { "type": "string", "format": "date-time" },
                "cohort_size": { "type": "integer", "example": 1420 },
                "cells": {
                  "type": "array",
                  "items": {
                    "type": "object",
                    "properties": {
                      "period": { "type": "integer", "example": 1 },
                      "retained_users": { "type": "integer", "example": 450 },
                      "percentage": { "type": "number", "example": 31.69 },
                      "is_future": { "type": "boolean", "example": false }
                    }
                  }
                }
              }
            }
          },
          "weighted_averages": { "type": "array", "items": { "type": "number" }, "example": [100.0, 32.4, 21.8, 16.5] }
        }
      },
      "SavedFunnel": {
        "type": "object",
        "properties": {
          "id": { "type": "integer", "example": 1 },
          "project_id": { "type": "integer", "example": 10 },
          "name": { "type": "string", "example": "E-Commerce Checkout" },
          "steps": { "type": "array", "items": { "type": "string" }, "example": ["view_product", "add_to_cart", "purchase"] },
          "window_seconds": { "type": "integer", "example": 86400 },
          "counting_mode": { "type": "string", "example": "users" },
          "breakdown_by": { "type": "string", "example": "utm_source" },
          "created_by": { "type": "integer", "example": 42 },
          "created_at": { "type": "string", "format": "date-time" },
          "updated_at": { "type": "string", "format": "date-time" }
        }
      },
      "SaveFunnelInput": {
        "type": "object",
        "required": ["name", "steps"],
        "properties": {
          "name": { "type": "string", "example": "E-Commerce Checkout" },
          "steps": { "type": "array", "items": { "type": "string" }, "example": ["view_product", "add_to_cart", "purchase"] },
          "window_seconds": { "type": "integer", "example": 86400, "default": 86400 },
          "counting_mode": { "type": "string", "enum": ["users", "sessions"], "default": "users" },
          "breakdown_by": { "type": "string", "example": "utm_source" }
        }
      },
      "AnalyticsCatalogResponse": {
        "type": "object",
        "properties": {
          "event_names": {
            "type": "array",
            "items": {
              "type": "object",
              "properties": {
                "name": { "type": "string", "example": "add_to_cart" },
                "count": { "type": "integer", "example": 14200 }
              }
            }
          },
          "utm_sources": { "type": "array", "items": { "type": "string" }, "example": ["google", "facebook", "newsletter"] },
          "browsers": { "type": "array", "items": { "type": "string" }, "example": ["Chrome", "Safari", "Firefox"] },
          "operating_systems": { "type": "array", "items": { "type": "string" }, "example": ["Android", "iOS", "macOS"] },
          "property_keys": { "type": "array", "items": { "type": "string" }, "example": ["plan", "price", "category"] }
        }
      }
    }
  }
}`)
