package handler

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5"
)

// artifactMaxDecoded caps an upload after gzip decoding, so a tiny gzip bomb
// cannot exhaust memory. A variable only so a test can lower it.
var artifactMaxDecoded int64 = 1 << 30

// readArtifactBody returns the upload body, gunzipped when the client sent
// Content-Encoding: gzip and unwrapped when sent as multipart/form-data. It
// reads the raw body: Fiber's c.Body() would decode gzip itself with no size limit.
func readArtifactBody(c *fiber.Ctx) ([]byte, error) {
	body, _, _, err := readArtifactUpload(c)
	return body, err
}

// readArtifactUpload decodes Content-Encoding: gzip and unwraps multipart/form-data
// uploads, returning the file payload, the multipart filename (if any), and any
// string form fields sent alongside the file.
func readArtifactUpload(c *fiber.Ctx) ([]byte, string, map[string]string, error) {
	raw := c.BodyRaw()
	var decoded []byte
	switch strings.ToLower(strings.TrimSpace(c.Get(fiber.HeaderContentEncoding))) {
	case "", "identity":
		decoded = raw
	case "gzip":
		var err error
		decoded, err = gunzipBounded(raw)
		if err != nil {
			return nil, "", nil, err
		}
	default:
		return nil, "", nil, fiber.NewError(fiber.StatusUnsupportedMediaType, "unsupported Content-Encoding (use gzip or none)")
	}

	payload, fileName, fields, ok, err := unwrapMultipartArtifact(decoded, c.Get(fiber.HeaderContentType))
	if err != nil {
		return nil, "", nil, err
	}
	if ok {
		return payload, fileName, fields, nil
	}
	return decoded, "", nil, nil
}

func gunzipBounded(raw []byte) ([]byte, error) {
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
}

// unwrapMultipartArtifact extracts the uploaded file part and form fields when
// raw is a multipart/form-data payload (detected either via Content-Type header
// or by a leading "--<boundary>\r\nContent-Disposition: form-data" envelope).
func unwrapMultipartArtifact(raw []byte, contentType string) ([]byte, string, map[string]string, bool, error) {
	boundary := ""
	if contentType != "" {
		if mediaType, params, err := mime.ParseMediaType(contentType); err == nil && strings.HasPrefix(strings.ToLower(mediaType), "multipart/") {
			boundary = params["boundary"]
		}
	}
	if boundary == "" && len(raw) > 4 && raw[0] == '-' && raw[1] == '-' {
		// Auto-detect multipart envelope when Content-Type header was omitted or stripped.
		firstLineEnd := bytes.IndexByte(raw, '\n')
		if firstLineEnd > 2 && firstLineEnd < 256 {
			firstLine := strings.TrimRight(string(raw[2:firstLineEnd]), "\r")
			rest := raw[firstLineEnd+1:]
			if len(rest) > 20 && bytes.HasPrefix(bytes.ToLower(rest), []byte("content-disposition:")) && firstLine != "" && !strings.ContainsAny(firstLine, " \t") {
				boundary = firstLine
			}
		}
	}
	if boundary == "" {
		return nil, "", nil, false, nil
	}

	mr := multipart.NewReader(bytes.NewReader(raw), boundary)
	fields := make(map[string]string)
	var filePayload []byte
	var fileName string
	hasFile := false

	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			if hasFile {
				break
			}
			return nil, "", nil, false, fiber.NewError(fiber.StatusBadRequest, "invalid multipart body: "+err.Error())
		}
		formName := part.FormName()
		partFile := part.FileName()
		data, readErr := io.ReadAll(io.LimitReader(part, artifactMaxDecoded+1))
		_ = part.Close()
		if readErr != nil {
			return nil, "", nil, false, fiber.NewError(fiber.StatusBadRequest, "cannot read multipart part")
		}
		if int64(len(data)) > artifactMaxDecoded {
			return nil, "", nil, false, fiber.NewError(fiber.StatusRequestEntityTooLarge, "decompressed body too large")
		}

		if partFile != "" || formName == "file" || formName == "dsym" || formName == "mapping" || formName == "sourcemap" || (!hasFile && formName == "") {
			// If the inner file part was gzipped (and isn't a zip/Mach-O), gunzip it.
			if len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b && !strings.HasSuffix(strings.ToLower(partFile), ".gz") {
				if gzOut, gzErr := gunzipBounded(data); gzErr == nil {
					data = gzOut
				}
			}
			filePayload = data
			fileName = partFile
			hasFile = true
		} else if formName != "" && len(data) < 4096 {
			fields[formName] = strings.TrimSpace(string(data))
		}
	}

	if !hasFile {
		return nil, "", fields, true, nil
	}
	return filePayload, fileName, fields, true, nil
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
