package handler

import (
	"bytes"
	"compress/gzip"
	"errors"

	"github.com/gofiber/fiber/v2"

	"github.com/tiofani03/artracker/backend/internal/ingest"
)

const appStartProfileShortLen = 64

// IngestAppStartProfile handles POST /api/v1/app-start/profiles: one startup profile per start_id,
// stored gzipped and symbolicated later by a worker. A duplicate start_id is accepted and ignored.
func (h *Handler) IngestAppStartProfile(c *fiber.Ctx) error {
	projectID := c.Locals("projectID").(int64)
	// c.Body() would gunzip on its own, without our size cap: read the raw bytes and decompress with a limit.
	body := c.Request().Body()

	reject := func(code int, msg string) error {
		if h.ingestSvc != nil {
			h.ingestSvc.Metrics().IncRejectedRequests()
		}
		return fiber.NewError(code, msg)
	}

	if len(body) == 0 {
		return reject(fiber.StatusBadRequest, "empty payload")
	}

	var rawJSON, stored []byte
	if c.Get("Content-Encoding") == "gzip" && isGzip(body) {
		if len(body) > ingest.MaxProfileCompressedBytes {
			return reject(fiber.StatusRequestEntityTooLarge, "payload exceeds maximum size")
		}
		out, err := h.ingestSvc.Decompressor().GunzipLimit(body, ingest.MaxProfileUncompressedBytes)
		if err != nil {
			if errors.Is(err, ingest.ErrPayloadTooLarge) {
				return reject(fiber.StatusRequestEntityTooLarge, "payload exceeds maximum size")
			}
			return reject(fiber.StatusBadRequest, "invalid gzip stream")
		}
		rawJSON, stored = out, body
	} else {
		if len(body) > ingest.MaxProfileUncompressedBytes {
			return reject(fiber.StatusRequestEntityTooLarge, "payload exceeds maximum size")
		}
		var buf bytes.Buffer
		zw := gzip.NewWriter(&buf)
		if _, err := zw.Write(body); err != nil {
			return err
		}
		if err := zw.Close(); err != nil {
			return err
		}
		rawJSON, stored = body, buf.Bytes()
	}

	upload, err := ingest.ParseAppStartProfile(rawJSON)
	if err != nil {
		return reject(fiber.StatusBadRequest, "invalid app start profile")
	}

	env, appVersion := ingest.EnvelopeEnvAndVersion(upload.Context)
	version, build := ingest.SplitVersionBuild(appVersion)
	if b, ok := upload.Context["build"].(string); ok && b != "" {
		build = b
	}
	startID := ingest.CleanStoredString(upload.Profile.StartID, ingest.MaxStringIDLen)
	env = ingest.CleanStoredString(env, appStartProfileShortLen)
	version = ingest.CleanStoredString(version, appStartProfileShortLen)
	build = ingest.CleanStoredString(build, appStartProfileShortLen)

	_, err = h.db.Exec(c.Context(), `
		INSERT INTO app_start_profiles (project_id, start_id, platform, environment, app_version, build, image_uuids, raw)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (project_id, start_id) DO NOTHING`,
		projectID, startID, upload.Profile.Platform, env, version, build, upload.Profile.ImageUUIDs(), stored)
	if err != nil {
		return err
	}

	h.ingestSvc.Metrics().IncAcceptedRequests()
	return c.Status(fiber.StatusAccepted).JSON(fiber.Map{"status": "accepted"})
}
