package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/tiofani03/artracker/backend/internal/analytics"
	"github.com/tiofani03/artracker/backend/internal/ingest"
)

// RateLimitAnalytics checks project-level request quota for the analytics endpoint.
func (h *Handler) RateLimitAnalytics(c *fiber.Ctx) error {
	projectID, ok := c.Locals("projectID").(int64)
	if !ok || projectID <= 0 {
		return fiber.NewError(fiber.StatusUnauthorized, "unauthorized")
	}

	if h.analyticsSvc == nil {
		return c.Next()
	}

	allowed, retryAfter := h.analyticsSvc.RateLimiter().AllowRequest(projectID)
	if !allowed {
		c.Set("Retry-After", strconv.Itoa(retryAfter))
		h.analyticsSvc.Metrics().IncRequestsRejected()
		return fiber.NewError(fiber.StatusTooManyRequests, "rate limit exceeded")
	}

	return c.Next()
}

// IngestEvents handles POST /api/v1/events batch ingestion for custom product & growth marketing events.
func (h *Handler) IngestEvents(c *fiber.Ctx) error {
	projectID := c.Locals("projectID").(int64)
	if h.analyticsSvc == nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, "analytics service unavailable")
	}

	body := c.Body()
	if len(body) == 0 {
		h.analyticsSvc.Metrics().IncRequestsRejected()
		return fiber.NewError(fiber.StatusBadRequest, "empty payload")
	}

	receivedAt := time.Now()
	var payloadBytes []byte

	// Check Content-Encoding: gzip
	contentEncoding := strings.ToLower(c.Get("Content-Encoding"))
	if contentEncoding == "gzip" {
		if len(body) > ingest.MaxCompressedBytes && isGzip(body) {
			h.analyticsSvc.Metrics().IncRequestsRejected()
			return fiber.NewError(fiber.StatusRequestEntityTooLarge, "payload exceeds maximum size")
		}

		if isGzip(body) {
			decompressed, err := h.ingestSvc.Decompressor().Gunzip(body)
			if err != nil {
				h.analyticsSvc.Metrics().IncRequestsRejected()
				if errors.Is(err, ingest.ErrPayloadTooLarge) {
					return fiber.NewError(fiber.StatusRequestEntityTooLarge, "payload exceeds uncompressed size limit")
				}
				return fiber.NewError(fiber.StatusBadRequest, "invalid gzip stream")
			}
			payloadBytes = decompressed
		} else {
			// Body was already decompressed by an upstream proxy/runtime
			trimmed := bytes.TrimSpace(body)
			if len(trimmed) > 0 && (trimmed[0] == '{' || trimmed[0] == '[') {
				if len(body) > ingest.MaxUncompressedBytes {
					h.analyticsSvc.Metrics().IncRequestsRejected()
					return fiber.NewError(fiber.StatusRequestEntityTooLarge, "payload exceeds size limit")
				}
				payloadBytes = body
			} else {
				h.analyticsSvc.Metrics().IncRequestsRejected()
				return fiber.NewError(fiber.StatusBadRequest, "invalid gzip stream")
			}
		}
	} else {
		if len(body) > ingest.MaxUncompressedBytes {
			h.analyticsSvc.Metrics().IncRequestsRejected()
			return fiber.NewError(fiber.StatusRequestEntityTooLarge, "payload exceeds size limit")
		}
		payloadBytes = body
	}

	var envelope analytics.EventEnvelope
	if err := json.Unmarshal(payloadBytes, &envelope); err != nil {
		h.analyticsSvc.Metrics().IncRequestsRejected()
		return fiber.NewError(fiber.StatusBadRequest, "malformed json payload")
	}

	eventCount := len(envelope.Events)
	if eventCount == 0 {
		h.analyticsSvc.Metrics().IncRequestsRejected()
		return fiber.NewError(fiber.StatusBadRequest, "events array is empty")
	}
	if eventCount > analytics.MaxBatchEvents {
		h.analyticsSvc.Metrics().IncRequestsRejected()
		return fiber.NewError(fiber.StatusBadRequest, "batch exceeds maximum items limit")
	}

	// Check Event-level rate limit
	allowed, retryAfter := h.analyticsSvc.RateLimiter().AllowEvents(projectID, eventCount)
	if !allowed {
		c.Set("Retry-After", strconv.Itoa(retryAfter))
		h.analyticsSvc.Metrics().IncRequestsRejected()
		return fiber.NewError(fiber.StatusTooManyRequests, "rate limit exceeded")
	}

	// Detach properties byte slice from fiber request buffer
	for i := range envelope.Events {
		if len(envelope.Events[i].Properties) > 0 {
			envelope.Events[i].Properties = bytes.Clone(envelope.Events[i].Properties)
		}
	}

	job := analytics.BatchJob{
		ProjectID:  projectID,
		ReceivedAt: receivedAt,
		Envelope:   envelope,
	}

	if !h.analyticsSvc.WorkerPool().Enqueue(job) {
		h.analyticsSvc.Metrics().IncRequestsRejected()
		c.Set("Retry-After", "5")
		return fiber.NewError(fiber.StatusServiceUnavailable, "analytics buffer full, retry later")
	}

	h.analyticsSvc.Metrics().IncRequestsAccepted()
	h.analyticsSvc.Metrics().AddEventsReceived(eventCount)

	return c.Status(fiber.StatusAccepted).JSON(fiber.Map{
		"status":       "accepted",
		"queued_items": eventCount,
	})
}

// GetAnalyticsCatalog returns unique event names, UTM values, browsers, and property keys.
func (h *Handler) GetAnalyticsCatalog(c *fiber.Ctx) error {
	projectID := c.Locals("projectID").(int64)
	if h.analyticsSvc == nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, "analytics service unavailable")
	}

	ctx, cancel := context.WithTimeout(c.Context(), 5*time.Second)
	defer cancel()

	catalog, err := h.analyticsSvc.GetCatalog(ctx, projectID)
	if err != nil {
		return err
	}
	return c.JSON(catalog)
}

// CalculateFunnel executes a conversion funnel calculation.
func (h *Handler) CalculateFunnel(c *fiber.Ctx) error {
	projectID := c.Locals("projectID").(int64)
	if h.analyticsSvc == nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, "analytics service unavailable")
	}

	var req analytics.FunnelQueryRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}

	ctx, cancel := context.WithTimeout(c.Context(), 5*time.Second)
	defer cancel()

	result, err := h.analyticsSvc.FunnelEngine().CalculateFunnel(ctx, projectID, req)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}

	return c.JSON(result)
}

// CalculateCohort calculates an N-day or N-week cohort retention matrix.
func (h *Handler) CalculateCohort(c *fiber.Ctx) error {
	projectID := c.Locals("projectID").(int64)
	if h.analyticsSvc == nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, "analytics service unavailable")
	}

	granularity := c.Query("granularity", "week")
	periods, _ := strconv.Atoi(c.Query("periods", "8"))
	startEvent := c.Query("start_event", "")
	returnEvent := c.Query("return_event", "")
	tz := c.Query("tz", "UTC")

	req := analytics.CohortQueryRequest{
		Granularity: granularity,
		Periods:     periods,
		StartEvent:  startEvent,
		ReturnEvent: returnEvent,
		Timezone:    tz,
	}

	ctx, cancel := context.WithTimeout(c.Context(), 5*time.Second)
	defer cancel()

	result, err := h.analyticsSvc.CohortEngine().CalculateCohort(ctx, projectID, req)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}

	return c.JSON(result)
}

// ListSavedFunnels returns team presets for a project.
func (h *Handler) ListSavedFunnels(c *fiber.Ctx) error {
	projectID := c.Locals("projectID").(int64)
	if h.analyticsSvc == nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, "analytics service unavailable")
	}

	ctx, cancel := context.WithTimeout(c.Context(), 5*time.Second)
	defer cancel()

	list, err := h.analyticsSvc.ListSavedFunnels(ctx, projectID)
	if err != nil {
		return err
	}
	if list == nil {
		list = []analytics.SavedFunnel{}
	}

	return c.JSON(fiber.Map{"items": list})
}

// CreateSavedFunnel persists a new funnel preset.
func (h *Handler) CreateSavedFunnel(c *fiber.Ctx) error {
	projectID := c.Locals("projectID").(int64)
	if h.analyticsSvc == nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, "analytics service unavailable")
	}

	var input analytics.SaveFunnelInput
	if err := c.BodyParser(&input); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}

	uid := userID(c)
	var uidPtr *int64
	if uid > 0 {
		uidPtr = &uid
	}

	ctx, cancel := context.WithTimeout(c.Context(), 5*time.Second)
	defer cancel()

	saved, err := h.analyticsSvc.CreateSavedFunnel(ctx, projectID, uidPtr, input)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}

	return c.Status(fiber.StatusCreated).JSON(saved)
}

// DeleteSavedFunnel removes a funnel preset.
func (h *Handler) DeleteSavedFunnel(c *fiber.Ctx) error {
	projectID := c.Locals("projectID").(int64)
	funnelID, err := strconv.ParseInt(c.Params("funnelID"), 10, 64)
	if err != nil || funnelID <= 0 {
		return fiber.NewError(fiber.StatusBadRequest, "invalid funnel id")
	}

	if h.analyticsSvc == nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, "analytics service unavailable")
	}

	ctx, cancel := context.WithTimeout(c.Context(), 5*time.Second)
	defer cancel()

	if err := h.analyticsSvc.DeleteSavedFunnel(ctx, projectID, funnelID); err != nil {
		return fiber.NewError(fiber.StatusNotFound, err.Error())
	}

	return c.SendStatus(fiber.StatusNoContent)
}
