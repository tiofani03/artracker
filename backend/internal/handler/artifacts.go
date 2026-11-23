package handler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5"

	"github.com/tiofani03/artracker/backend/internal/appstart"
	"github.com/tiofani03/artracker/backend/internal/issues"
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

	body, partFileName, formFields, err := readArtifactUpload(c)
	if err != nil {
		return err
	}
	if len(body) == 0 {
		return fiber.NewError(fiber.StatusBadRequest, "dSYM binary body or zip archive is required")
	}

	fileName := strings.TrimSpace(c.Query("file"))
	if fileName == "" {
		fileName = strings.TrimSpace(partFileName)
	}
	if fileName == "" && formFields["bundle_name"] != "" {
		fileName = formFields["bundle_name"]
	}
	if fileName == "" {
		fileName = "app.dSYM"
	}

	dwarfBytes := body

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

	checksum := sha256Hex(dwarfBytes)

	storagePath := fmt.Sprintf("projects/%d/releases/%s/dsyms/%s", projectID, version, filepath.Base(fileName))

	if h.db == nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, "database is unavailable")
	}

	ctx, cancel := context.WithTimeout(c.Context(), 60*time.Second)
	defer cancel()

	if same, err := h.isLatestArtifact(ctx, projectID, version, "dsym", checksum); err != nil {
		return err
	} else if same {
		return c.JSON(fiber.Map{
			"status":          "unchanged",
			"project_id":      projectID,
			"release_version": version,
			"file_name":       fileName,
			"debug_uuid":      meta.UUID,
			"cpu_arch":        meta.Arch,
			"size_bytes":      len(dwarfBytes),
			"checksum":        checksum,
		})
	}

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

	h.requeueAppStartProfiles(ctx, projectID, "", meta.UUID)

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

	body, partFileName, formFields, err := readArtifactUpload(c)
	if err != nil {
		return err
	}
	if len(body) == 0 {
		return fiber.NewError(fiber.StatusBadRequest, "mapping.txt content is required")
	}

	// Validate mapping syntax
	_, err = symbolication.ParseProguardMapping(body)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid proguard mapping: "+err.Error())
	}

	fileName := strings.TrimSpace(c.Query("file"))
	if fileName == "" {
		fileName = strings.TrimSpace(partFileName)
	}
	if fileName == "" {
		fileName = "mapping.txt"
	}
	buildNumber := strings.TrimSpace(c.Query("build"))
	if buildNumber == "" {
		buildNumber = formFields["build"]
	}

	checksum := sha256Hex(body)

	storagePath := fmt.Sprintf("projects/%d/releases/%s/proguard/%s", projectID, version, filepath.Base(fileName))

	if h.db == nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, "database is unavailable")
	}

	ctx, cancel := context.WithTimeout(c.Context(), 60*time.Second)
	defer cancel()

	if same, err := h.isLatestArtifact(ctx, projectID, version, "proguard_mapping", checksum); err != nil {
		return err
	} else if same {
		return c.JSON(fiber.Map{
			"status":          "unchanged",
			"project_id":      projectID,
			"release_version": version,
			"file_name":       fileName,
			"artifact_type":   "proguard_mapping",
			"size_bytes":      len(body),
			"checksum":        checksum,
		})
	}

	var artifactID int64
	err = h.db.QueryRow(ctx, `
		INSERT INTO project_release_artifacts(
			project_id, version, build_number, artifact_type, file_name,
			storage_path, content, checksum_sha256, cpu_arch, debug_uuid, size_bytes
		)
		VALUES ($1, $2, $3, 'proguard_mapping', $4, $5, $6, $7, '', '', $8)
		RETURNING id`,
		projectID, version, buildNumber, fileName,
		storagePath, body, checksum, int64(len(body)),
	).Scan(&artifactID)
	if err != nil {
		return err
	}

	h.requeueAppStartProfiles(ctx, projectID, version, "")

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

// requeueAppStartProfiles sends app start profiles that were waiting on symbols back to the worker once a
// matching artifact is stored: version for a mapping, debugUUID for a dSYM. The upload already succeeded,
// so a failure only logs.
func (h *Handler) requeueAppStartProfiles(ctx context.Context, projectID int64, version, debugUUID string) {
	if err := appstart.Requeue(ctx, h.db, projectID, version, debugUUID); err != nil {
		slog.Warn("requeue app start profiles failed", "err", err, "project_id", projectID, "version", version, "debug_uuid", debugUUID)
	}
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

// UploadSymbols handles POST /api/v1/symbols and POST /api/v1/symbols/:version.
// It auto-detects iOS dSYM (.zip or Mach-O binary), Android ProGuard (mapping.txt),
// or Source Map, and stores it under the authenticated project with no extra login.
func (h *Handler) UploadSymbols(c *fiber.Ctx) error {
	projectID, ok := c.Locals("projectID").(int64)
	if !ok || projectID <= 0 {
		return fiber.NewError(fiber.StatusUnauthorized, "unauthorized")
	}

	body, partFileName, formFields, err := readArtifactUpload(c)
	if err != nil {
		return err
	}

	rawVersion, _ := url.PathUnescape(c.Params("version"))
	version := strings.TrimSpace(rawVersion)
	if version == "" {
		version = strings.TrimSpace(c.Query("version"))
	}
	if version == "" {
		version = strings.TrimSpace(c.Query("release"))
	}
	if version == "" {
		version = strings.TrimSpace(c.Query("app_version"))
	}
	if version == "" {
		version = strings.TrimSpace(c.Get("X-App-Version"))
	}
	if version == "" {
		version = strings.TrimSpace(c.Get("X-Release-Version"))
	}
	if version == "" {
		version = formFields["version"]
	}
	if version == "" {
		version = formFields["app_version"]
	}
	if version == "" {
		return fiber.NewError(fiber.StatusBadRequest, "release version is required")
	}

	if len(body) == 0 {
		return fiber.NewError(fiber.StatusBadRequest, "symbols body or archive is required")
	}

	buildNumber := strings.TrimSpace(c.Query("build"))
	if buildNumber == "" {
		buildNumber = strings.TrimSpace(c.Get("X-App-Build"))
	}
	if buildNumber == "" {
		buildNumber = formFields["build"]
	}

	explicitFileName := strings.TrimSpace(c.Query("file"))
	if explicitFileName == "" {
		explicitFileName = strings.TrimSpace(c.Get("X-File-Name"))
	}
	fileName := explicitFileName
	if fileName == "" {
		fileName = strings.TrimSpace(partFileName)
	}
	if fileName == "" {
		fileName = formFields["bundle_name"]
	}

	artifactType := strings.ToLower(strings.TrimSpace(c.Query("type")))
	if artifactType == "" {
		artifactType = strings.ToLower(strings.TrimSpace(c.Get("X-Artifact-Type")))
	}
	if artifactType == "" {
		artifactType = strings.ToLower(formFields["type"])
	}

	isZip := len(body) >= 4 && body[0] == 'P' && body[1] == 'K' && body[2] == 0x03 && body[3] == 0x04
	_, machOErr := symbolication.ExtractMachOMetadata(body)
	isMachO := machOErr == nil

	// Route to dSYM if zip archive, Mach-O binary, or explicitly dsym
	if artifactType == "dsym" || isZip || isMachO || strings.HasSuffix(strings.ToLower(fileName), ".dsym") || strings.HasSuffix(strings.ToLower(fileName), ".zip") || strings.HasSuffix(strings.ToLower(formFields["bundle_name"]), ".dsym") {
		dwarfBytes := body
		if isZip {
			innerBytes, innerName, zipErr := symbolication.ExtractDSYMBinaryFromZip(body)
			if zipErr != nil {
				return fiber.NewError(fiber.StatusBadRequest, "invalid dSYM zip: "+zipErr.Error())
			}
			dwarfBytes = innerBytes
			if innerName != "" && explicitFileName == "" {
				fileName = innerName
			}
		}
		if fileName == "" {
			fileName = "app.dSYM"
		}
		meta, metaErr := symbolication.ExtractMachOMetadata(dwarfBytes)
		if metaErr != nil {
			meta = symbolication.MachOInfo{Arch: "arm64"}
		}
		checksum := sha256Hex(dwarfBytes)
		storagePath := fmt.Sprintf("projects/%d/releases/%s/dsyms/%s", projectID, version, filepath.Base(fileName))

		if h.db == nil {
			return fiber.NewError(fiber.StatusServiceUnavailable, "database is unavailable")
		}
		ctx, cancel := context.WithTimeout(c.Context(), 60*time.Second)
		defer cancel()

		if same, err := h.isLatestArtifact(ctx, projectID, version, "dsym", checksum); err != nil {
			return err
		} else if same {
			return c.JSON(fiber.Map{
				"status":          "unchanged",
				"project_id":      projectID,
				"release_version": version,
				"file_name":       fileName,
				"debug_uuid":      meta.UUID,
				"cpu_arch":        meta.Arch,
				"size_bytes":      len(dwarfBytes),
				"checksum":        checksum,
			})
		}

		var artifactID int64
		err = h.db.QueryRow(ctx, `
			INSERT INTO project_release_artifacts(
				project_id, version, build_number, artifact_type, file_name,
				storage_path, content, checksum_sha256, cpu_arch, debug_uuid, size_bytes
			)
			VALUES ($1, $2, $3, 'dsym', $4, $5, $6, $7, $8, $9, $10)
			RETURNING id`,
			projectID, version, buildNumber, fileName,
			storagePath, dwarfBytes, checksum, meta.Arch, meta.UUID, int64(len(dwarfBytes)),
		).Scan(&artifactID)
		if err != nil {
			return err
		}

		h.requeueAppStartProfiles(ctx, projectID, "", meta.UUID)

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

	// Route to SourceMap if json format or explicitly sourcemap
	if artifactType == "sourcemap" || strings.HasSuffix(strings.ToLower(fileName), ".map") {
		sm, err := issues.ParseSourceMap(body)
		if err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "invalid source map: "+err.Error())
		}
		if fileName == "" {
			fileName = strings.TrimSpace(sm.File)
		}
		if fileName == "" {
			return fiber.NewError(fiber.StatusBadRequest, "file name is required (source map has no 'file' field)")
		}

		ctx, cancel := context.WithTimeout(c.Context(), 60*time.Second)
		defer cancel()

		var current string
		err = h.db.QueryRow(ctx, `SELECT encode(sha256(content), 'hex') FROM source_maps
			WHERE project_id = $1 AND release_version = $2 AND file_name = $3`, projectID, version, fileName).Scan(&current)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if err == nil && current == sha256Hex(body) {
			return c.JSON(fiber.Map{
				"status":          "unchanged",
				"release_version": version,
				"file_name":       fileName,
				"sources":         len(sm.Sources),
			})
		}

		if _, err := h.db.Exec(ctx, `
			INSERT INTO source_maps(project_id, release_version, file_name, content)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (project_id, release_version, file_name)
			DO UPDATE SET content = EXCLUDED.content, created_at = now()`,
			projectID, version, fileName, body); err != nil {
			return err
		}

		return c.Status(fiber.StatusCreated).JSON(fiber.Map{
			"status":          "stored",
			"project_id":      projectID,
			"release_version": version,
			"file_name":       fileName,
			"sources":         len(sm.Sources),
		})
	}

	// Default/fallback: ProGuard / R8 mapping file
	_, err = symbolication.ParseProguardMapping(body)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid proguard mapping or unrecognized symbols format: "+err.Error())
	}

	if fileName == "" {
		fileName = "mapping.txt"
	}
	checksum := sha256Hex(body)
	storagePath := fmt.Sprintf("projects/%d/releases/%s/proguard/%s", projectID, version, filepath.Base(fileName))

	if h.db == nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, "database is unavailable")
	}
	ctx, cancel := context.WithTimeout(c.Context(), 60*time.Second)
	defer cancel()

	if same, err := h.isLatestArtifact(ctx, projectID, version, "proguard_mapping", checksum); err != nil {
		return err
	} else if same {
		return c.JSON(fiber.Map{
			"status":          "unchanged",
			"project_id":      projectID,
			"release_version": version,
			"file_name":       fileName,
			"artifact_type":   "proguard_mapping",
			"size_bytes":      len(body),
			"checksum":        checksum,
		})
	}

	var artifactID int64
	err = h.db.QueryRow(ctx, `
		INSERT INTO project_release_artifacts(
			project_id, version, build_number, artifact_type, file_name,
			storage_path, content, checksum_sha256, cpu_arch, debug_uuid, size_bytes
		)
		VALUES ($1, $2, $3, 'proguard_mapping', $4, $5, $6, $7, '', '', $8)
		RETURNING id`,
		projectID, version, buildNumber, fileName,
		storagePath, body, checksum, int64(len(body)),
	).Scan(&artifactID)
	if err != nil {
		return err
	}

	h.requeueAppStartProfiles(ctx, projectID, version, "")

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
