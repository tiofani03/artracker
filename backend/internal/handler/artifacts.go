package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/tiofani03/artracker/backend/internal/symbolication"
)

// ReleaseArtifactItem represents an uploaded build debug artifact.
type ReleaseArtifactItem struct {
	ID             int64     `json:"id"`
	ProjectID      int64     `json:"project_id"`
	Version        string    `json:"version"`
	BuildNumber    string    `json:"build_number"`
	ArtifactType   string    `json:"artifact_type"`
	FileName       string    `json:"file_name"`
	StoragePath    string    `json:"storage_path"`
	ChecksumSHA256 string    `json:"checksum_sha256"`
	CPUArch        string    `json:"cpu_arch,omitempty"`
	DebugUUID      string    `json:"debug_uuid,omitempty"`
	SizeBytes      int64     `json:"size_bytes"`
	CreatedAt      time.Time `json:"created_at"`
}

// UploadDSYM handles POST /api/v1/projects/:projectID/releases/:version/dsyms.
// It accepts a .dSYM.zip archive or a raw Mach-O DWARF binary file.
func (h *Handler) UploadDSYM(c *fiber.Ctx) error {
	projectID, ok := c.Locals("projectID").(int64)
	if !ok || projectID <= 0 {
		return fiber.NewError(fiber.StatusUnauthorized, "unauthorized")
	}

	rawVersion, _ := url.PathUnescape(c.Params("version"))
	version := strings.TrimSpace(rawVersion)
	if version == "" {
		return fiber.NewError(fiber.StatusBadRequest, "release version is required")
	}

	body := c.Body()
	if len(body) == 0 {
		return fiber.NewError(fiber.StatusBadRequest, "dSYM binary body or zip archive is required")
	}

	fileName := strings.TrimSpace(c.Query("file"))
	if fileName == "" {
		fileName = "app.dSYM"
	}

	dwarfBytes := body
	var err error

	// If payload is a zip archive (starts with PK\x03\x04), extract the inner DWARF binary
	if len(body) >= 4 && body[0] == 'P' && body[1] == 'K' && body[2] == 0x03 && body[3] == 0x04 {
		innerBytes, innerName, zipErr := symbolication.ExtractDSYMBinaryFromZip(body)
		if zipErr != nil {
			return fiber.NewError(fiber.StatusBadRequest, "invalid dSYM zip: "+zipErr.Error())
		}
		dwarfBytes = innerBytes
		if innerName != "" {
			fileName = innerName
		}
	}

	// Extract Mach-O metadata (UUID, architecture)
	meta, metaErr := symbolication.ExtractMachOMetadata(dwarfBytes)
	if metaErr != nil {
		// Log warning but continue if raw binary
		meta = symbolication.MachOInfo{Arch: "arm64"}
	}

	hasher := sha256.New()
	hasher.Write(dwarfBytes)
	checksum := hex.EncodeToString(hasher.Sum(nil))

	storagePath := fmt.Sprintf("projects/%d/releases/%s/dsyms/%s", projectID, version, filepath.Base(fileName))

	if h.db == nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, "database is unavailable")
	}

	ctx, cancel := context.WithTimeout(c.Context(), 10*time.Second)
	defer cancel()

	var artifactID int64
	err = h.db.QueryRow(ctx, `
		INSERT INTO project_release_artifacts(
			project_id, version, build_number, artifact_type, file_name,
			storage_path, content, checksum_sha256, cpu_arch, debug_uuid, size_bytes
		)
		VALUES ($1, $2, $3, 'dsym', $4, $5, $6, $7, $8, $9, $10)
		RETURNING id`,
		projectID, version, strings.TrimSpace(c.Query("build")), fileName,
		storagePath, dwarfBytes, checksum, meta.Arch, meta.UUID, int64(len(dwarfBytes)),
	).Scan(&artifactID)
	if err != nil {
		return err
	}

	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"status":          "stored",
		"id":              artifactID,
		"project_id":      projectID,
		"release_version": version,
		"file_name":       fileName,
		"debug_uuid":      meta.UUID,
		"cpu_arch":        meta.Arch,
		"size_bytes":      len(dwarfBytes),
		"checksum":        checksum,
	})
}

// UploadProguard handles POST /api/v1/projects/:projectID/releases/:version/proguard.
// It accepts a ProGuard / R8 mapping.txt file.
func (h *Handler) UploadProguard(c *fiber.Ctx) error {
	projectID, ok := c.Locals("projectID").(int64)
	if !ok || projectID <= 0 {
		return fiber.NewError(fiber.StatusUnauthorized, "unauthorized")
	}

	rawVersion, _ := url.PathUnescape(c.Params("version"))
	version := strings.TrimSpace(rawVersion)
	if version == "" {
		return fiber.NewError(fiber.StatusBadRequest, "release version is required")
	}

	body := c.Body()
	if len(body) == 0 {
		return fiber.NewError(fiber.StatusBadRequest, "mapping.txt content is required")
	}

	// Validate mapping syntax
	retracer, err := symbolication.ParseProguardMapping(body)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid proguard mapping: "+err.Error())
	}

	fileName := strings.TrimSpace(c.Query("file"))
	if fileName == "" {
		fileName = "mapping.txt"
	}

	hasher := sha256.New()
	hasher.Write(body)
	checksum := hex.EncodeToString(hasher.Sum(nil))

	storagePath := fmt.Sprintf("projects/%d/releases/%s/proguard/%s", projectID, version, filepath.Base(fileName))

	if h.db == nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, "database is unavailable")
	}

	ctx, cancel := context.WithTimeout(c.Context(), 5*time.Second)
	defer cancel()

	var artifactID int64
	err = h.db.QueryRow(ctx, `
		INSERT INTO project_release_artifacts(
			project_id, version, build_number, artifact_type, file_name,
			storage_path, content, checksum_sha256, cpu_arch, debug_uuid, size_bytes
		)
		VALUES ($1, $2, $3, 'proguard_mapping', $4, $5, $6, $7, '', '', $8)
		RETURNING id`,
		projectID, version, strings.TrimSpace(c.Query("build")), fileName,
		storagePath, body, checksum, int64(len(body)),
	).Scan(&artifactID)
	if err != nil {
		return err
	}

	_ = retracer

	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"status":          "stored",
		"id":              artifactID,
		"project_id":      projectID,
		"release_version": version,
		"file_name":       fileName,
		"artifact_type":   "proguard_mapping",
		"size_bytes":      len(body),
		"checksum":        checksum,
	})
}

// ListReleaseArtifacts handles GET /api/v1/projects/:projectID/releases/:version/artifacts.
func (h *Handler) ListReleaseArtifacts(c *fiber.Ctx) error {
	projectID, ok := c.Locals("projectID").(int64)
	if !ok || projectID <= 0 {
		return fiber.NewError(fiber.StatusUnauthorized, "unauthorized")
	}

	rawVersion, _ := url.PathUnescape(c.Params("version"))
	version := strings.TrimSpace(rawVersion)
	if version == "" {
		return fiber.NewError(fiber.StatusBadRequest, "release version is required")
	}

	if h.db == nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, "database is unavailable")
	}

	ctx, cancel := context.WithTimeout(c.Context(), 5*time.Second)
	defer cancel()

	rows, err := h.db.Query(ctx, `
		SELECT id, project_id, version, build_number, artifact_type, file_name,
		       storage_path, checksum_sha256, cpu_arch, debug_uuid, size_bytes, created_at
		FROM project_release_artifacts
		WHERE project_id = $1 AND version = $2
		ORDER BY created_at DESC`,
		projectID, version,
	)
	if err != nil {
		return err
	}
	defer rows.Close()

	items := []ReleaseArtifactItem{}
	for rows.Next() {
		var item ReleaseArtifactItem
		if err := rows.Scan(
			&item.ID, &item.ProjectID, &item.Version, &item.BuildNumber, &item.ArtifactType,
			&item.FileName, &item.StoragePath, &item.ChecksumSHA256, &item.CPUArch,
			&item.DebugUUID, &item.SizeBytes, &item.CreatedAt,
		); err != nil {
			return err
		}
		items = append(items, item)
	}

	return c.JSON(fiber.Map{
		"project_id": projectID,
		"version":    version,
		"artifacts":  items,
	})
}
