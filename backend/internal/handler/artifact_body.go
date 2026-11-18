package handler

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5"
)

// artifactMaxDecoded caps an upload after gzip decoding, so a tiny gzip bomb
// cannot exhaust memory. A variable only so a test can lower it.
var artifactMaxDecoded int64 = 1 << 30

// readArtifactBody returns the upload body, gunzipped when the client sent
// Content-Encoding: gzip. It reads the raw body: Fiber's c.Body() would decode
// gzip itself with no size limit.
func readArtifactBody(c *fiber.Ctx) ([]byte, error) {
	raw := c.BodyRaw()
	switch strings.ToLower(strings.TrimSpace(c.Get(fiber.HeaderContentEncoding))) {
	case "", "identity":
		return raw, nil
	case "gzip":
		zr, err := gzip.NewReader(bytes.NewReader(raw))
		if err != nil {
			return nil, fiber.NewError(fiber.StatusBadRequest, "invalid gzip body")
		}
		defer zr.Close()
		out, err := io.ReadAll(io.LimitReader(zr, artifactMaxDecoded+1))
		if err != nil {
			return nil, fiber.NewError(fiber.StatusBadRequest, "invalid gzip body")
		}
		if int64(len(out)) > artifactMaxDecoded {
			return nil, fiber.NewError(fiber.StatusRequestEntityTooLarge, "decompressed body too large")
		}
		return out, nil
	default:
		return nil, fiber.NewError(fiber.StatusUnsupportedMediaType, "unsupported Content-Encoding (use gzip or none)")
	}
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// isLatestArtifact reports whether the newest stored artifact of this type for
// the release already has this checksum. Symbolication always uses the newest
// one, so only then is a re-upload a no-op: after A then B, uploading A again
// must be stored to make A the active mapping again.
func (h *Handler) isLatestArtifact(ctx context.Context, projectID int64, version, artifactType, checksum string) (bool, error) {
	var latest string
	err := h.db.QueryRow(ctx, `
		SELECT checksum_sha256 FROM project_release_artifacts
		WHERE project_id = $1 AND version = $2 AND artifact_type = $3
		ORDER BY created_at DESC, id DESC LIMIT 1`, projectID, version, artifactType).Scan(&latest)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return err == nil && latest == checksum, err
}
