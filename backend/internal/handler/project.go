package handler

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5"

	"github.com/tiofani03/artracker/backend/internal/alerts"
	"github.com/tiofani03/artracker/backend/internal/auth"
)

type Project struct {
	ID        int64     `json:"id"`
	OrgID     int64     `json:"org_id"`
	Name      string    `json:"name"`
	Slug      string    `json:"slug"`
	Platform  string    `json:"platform"`
	CreatedAt time.Time `json:"created_at"`

	// ReplayScreenshotEnabled is the server-side switch for screenshot-mode replay uploads.
	ReplayScreenshotEnabled bool `json:"replay_screenshot_enabled"`
}

const projectColumns = `id, org_id, name, slug, platform, created_at, replay_screenshot_enabled`

func scanProject(row pgx.Row, p *Project) error {
	return row.Scan(&p.ID, &p.OrgID, &p.Name, &p.Slug, &p.Platform, &p.CreatedAt, &p.ReplayScreenshotEnabled)
}

func (h *Handler) ListProjects(c *fiber.Ctx) error {
	ctx, cancel := context.WithTimeout(c.Context(), 5*time.Second)
	defer cancel()
	rows, err := h.db.Query(ctx, `SELECT `+projectColumns+` FROM projects WHERE org_id = $1 ORDER BY created_at DESC, id DESC`,
		c.Locals("orgID"))
	if err != nil {
		return err
	}
	defer rows.Close()
	items := []Project{}
	for rows.Next() {
		var p Project
		if err := scanProject(rows, &p); err != nil {
			return err
		}
		items = append(items, p)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return c.JSON(fiber.Map{"items": items})
}

func (h *Handler) CreateProject(c *fiber.Ctx) error {
	var in struct {
		Name     string `json:"name"`
		Platform string `json:"platform"`
	}
	if err := c.BodyParser(&in); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid json body")
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return fiber.NewError(fiber.StatusBadRequest, "name is required")
	}
	slug := slugify(name)
	if slug == "" {
		slug = fallbackSlug("project")
	}
	switch in.Platform {
	case "web", "android", "ios":
	default:
		return fiber.NewError(fiber.StatusBadRequest, "platform must be web, android or ios")
	}

	ctx, cancel := context.WithTimeout(c.Context(), 5*time.Second)
	defer cancel()
	p, err := h.insertProject(ctx, c.Locals("orgID"), name, slug, in.Platform, seedAlertRules)
	if isUniqueViolation(err) {
		return errSlugTaken
	}
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(p)
}

// seedAlertRules creates the default alert rules of a new project.
func seedAlertRules(ctx context.Context, tx pgx.Tx, projectID int64) error {
	_, err := tx.Exec(ctx, alerts.SeedRulesSQL, projectID)
	return err
}

// insertProject inserts the project and runs seed in one transaction, so a
// project never exists without its default alert rules (it would never alert,
// and a retry would hit a slug conflict).
func (h *Handler) insertProject(ctx context.Context, orgID any, name, slug, platform string,
	seed func(context.Context, pgx.Tx, int64) error) (Project, error) {
	var p Project
	tx, err := h.db.Begin(ctx)
	if err != nil {
		return p, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after commit
	err = scanProject(tx.QueryRow(ctx,
		`INSERT INTO projects(org_id, name, slug, platform) VALUES ($1, $2, $3, $4) RETURNING `+projectColumns,
		orgID, name, slug, platform), &p)
	if err != nil {
		return p, err
	}
	if err := seed(ctx, tx, p.ID); err != nil {
		return p, fmt.Errorf("seed alert rules for project %d: %w", p.ID, err)
	}
	return p, tx.Commit(ctx)
}

var errProjectNotFound = fiber.NewError(fiber.StatusNotFound, "project not found")

// projectRole returns the caller's role in the org that owns the project, or
// pgx.ErrNoRows when the project doesn't exist or the caller isn't a member.
func (h *Handler) projectRole(ctx context.Context, uid, projectID int64) (auth.Role, error) {
	var role string
	err := h.db.QueryRow(ctx, `
		SELECT m.role FROM projects p
		JOIN organization_members m ON m.org_id = p.org_id AND m.user_id = $1
		WHERE p.id = $2`, uid, projectID).Scan(&role)
	return auth.Role(role), err
}

func (h *Handler) GetProject(c *fiber.Ctx) error {
	id, err := strconv.ParseInt(c.Params("projectID"), 10, 64)
	if err != nil {
		return errProjectNotFound
	}
	ctx, cancel := context.WithTimeout(c.Context(), 5*time.Second)
	defer cancel()
	if _, err := h.projectRole(ctx, userID(c), id); errors.Is(err, pgx.ErrNoRows) {
		return errProjectNotFound
	} else if err != nil {
		return err
	}
	var p Project
	if err := scanProject(h.db.QueryRow(ctx, `SELECT `+projectColumns+` FROM projects WHERE id = $1`, id), &p); err != nil {
		return err
	}
	return c.JSON(p)
}
