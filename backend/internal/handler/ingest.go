package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/tiofani03/artracker/backend/internal/ingest"
)

// VerifyIngestKey authenticates incoming requests using the Ingest key cache.
func (h *Handler) VerifyIngestKey(c *fiber.Ctx) error {
	rawKey := c.Get("X-Artracker-Key")
	if rawKey == "" {
		rawKey = c.Query("api_key")
	}
	if rawKey == "" {
		if h.ingestSvc != nil {
			h.ingestSvc.Metrics().IncRejectedRequests()
		}
		return fiber.NewError(fiber.StatusUnauthorized, "missing api key")
	}

	if h.ingestSvc == nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, "ingest service unavailable")
	}

	info, err := h.ingestSvc.KeyCache().Resolve(c.Context(), rawKey)
	if err != nil {
		h.ingestSvc.Metrics().IncRejectedRequests()
		return fiber.NewError(fiber.StatusUnauthorized, "invalid api key")
	}

	c.Locals("projectID", info.ProjectID)
	c.Locals("keyType", info.KeyType)
	return c.Next()
}

// RateLimitIngest checks project-level request quota.
func (h *Handler) RateLimitIngest(c *fiber.Ctx) error {
	projectID, ok := c.Locals("projectID").(int64)
	if !ok || projectID <= 0 {
		return fiber.NewError(fiber.StatusUnauthorized, "unauthorized")
	}

	if h.ingestSvc == nil {
		return c.Next()
	}

	allowed, retryAfter := h.ingestSvc.RateLimiter().AllowRequest(projectID)
	if !allowed {
		c.Set("Retry-After", strconv.Itoa(retryAfter))
		h.ingestSvc.Metrics().IncRejectedRequests()
		return fiber.NewError(fiber.StatusTooManyRequests, "rate limit exceeded")
	}

	return c.Next()
}

// Ingest handles POST /api/v1/ingest.
func (h *Handler) Ingest(c *fiber.Ctx) error {
	projectID := c.Locals("projectID").(int64)
	body := c.Body()

	if len(body) == 0 {
		if h.ingestSvc != nil {
			h.ingestSvc.Metrics().IncRejectedRequests()
		}
		return fiber.NewError(fiber.StatusBadRequest, "empty payload")
	}

	var rawJSON []byte
	contentEncoding := c.Get("Content-Encoding")
	if contentEncoding == "gzip" {
		if len(body) > ingest.MaxCompressedBytes && isGzip(body) {
			if h.ingestSvc != nil {
				h.ingestSvc.Metrics().IncRejectedRequests()
			}
			return fiber.NewError(fiber.StatusRequestEntityTooLarge, "payload exceeds maximum size")
		}

		if isGzip(body) {
			decompressed, err := h.ingestSvc.Decompressor().Gunzip(body)
			if err != nil {
				if h.ingestSvc != nil {
					h.ingestSvc.Metrics().IncRejectedRequests()
				}
				if errors.Is(err, ingest.ErrPayloadTooLarge) {
					return fiber.NewError(fiber.StatusRequestEntityTooLarge, "payload exceeds maximum size")
				}
				return fiber.NewError(fiber.StatusBadRequest, "invalid gzip stream")
			}
			rawJSON = decompressed
		} else {
			// Body was already decompressed by an upstream proxy/runtime
			trimmed := bytes.TrimSpace(body)
			if len(trimmed) > 0 && (trimmed[0] == '{' || trimmed[0] == '[') {
				if len(body) > ingest.MaxUncompressedBytes {
					if h.ingestSvc != nil {
						h.ingestSvc.Metrics().IncRejectedRequests()
					}
					return fiber.NewError(fiber.StatusRequestEntityTooLarge, "payload exceeds maximum size")
				}
				rawJSON = body
			} else {
				if h.ingestSvc != nil {
					h.ingestSvc.Metrics().IncRejectedRequests()
				}
				return fiber.NewError(fiber.StatusBadRequest, "invalid gzip stream")
			}
		}
	} else {
		if len(body) > ingest.MaxUncompressedBytes {
			if h.ingestSvc != nil {
				h.ingestSvc.Metrics().IncRejectedRequests()
			}
			return fiber.NewError(fiber.StatusRequestEntityTooLarge, "payload exceeds maximum size")
		}
		rawJSON = body
	}

	var envelope ingest.IngestEnvelope
	if err := json.Unmarshal(rawJSON, &envelope); err != nil {
		if h.ingestSvc != nil {
			h.ingestSvc.Metrics().IncRejectedRequests()
		}
		return fiber.NewError(fiber.StatusBadRequest, "invalid json payload")
	}

	if len(envelope.Items) == 0 {
		if h.ingestSvc != nil {
			h.ingestSvc.Metrics().IncRejectedRequests()
		}
		return fiber.NewError(fiber.StatusBadRequest, "items array cannot be empty")
	}

	if len(envelope.Items) > ingest.MaxBatchItems {
		if h.ingestSvc != nil {
			h.ingestSvc.Metrics().IncRejectedRequests()
		}
		return fiber.NewError(fiber.StatusBadRequest, "batch item count exceeds maximum limit")
	}

	// Project Event rate limiting
	if h.ingestSvc != nil {
		allowed, retryAfter := h.ingestSvc.RateLimiter().AllowEvents(projectID, len(envelope.Items))
		if !allowed {
			c.Set("Retry-After", strconv.Itoa(retryAfter))
			h.ingestSvc.Metrics().IncRejectedRequests()
			return fiber.NewError(fiber.StatusTooManyRequests, "event rate limit exceeded")
		}
	}

	// Detach byte slices from fasthttp request memory buffer
	for i := range envelope.Items {
		if len(envelope.Items[i].Attributes) > 0 {
			envelope.Items[i].Attributes = bytes.Clone(envelope.Items[i].Attributes)
		}
	}

	job := ingest.BatchJob{
		ProjectID:  projectID,
		ReceivedAt: time.Now(),
		Envelope:   envelope,
	}

	if h.ingestSvc != nil {
		if !h.ingestSvc.WorkerPool().Enqueue(job) {
			c.Set("Retry-After", "5")
			h.ingestSvc.Metrics().IncRejectedRequests()
			return fiber.NewError(fiber.StatusServiceUnavailable, "ingest buffer full, retry later")
		}
		h.ingestSvc.Metrics().IncAcceptedRequests()
		h.ingestSvc.Metrics().AddEventsReceived(len(envelope.Items))
	}

	return c.Status(fiber.StatusAccepted).JSON(fiber.Map{
		"status": "accepted",
		"queued": len(envelope.Items),
	})
}

func isGzip(data []byte) bool {
	return len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b
}
