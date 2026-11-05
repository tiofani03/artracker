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
          "413": { "description": "Chunk size exceeds maximum limit (5MB)" },
          "503": { "description": "Object storage not configured" }
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
          { "name": "user_id", "in": "query", "required": false, "schema": { "type": "string" } },
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
          "start_time": { "type": "string", "format": "date-time" }
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
          "has_rage_click": { "type": "boolean", "example": false }
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
      }
    }
  }
}`)
