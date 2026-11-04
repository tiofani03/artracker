package handler

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/tiofani03/artracker/backend/internal/auth"
)

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// slugify lowercases, collapses non [a-z0-9] runs to "-", trims "-" and caps at 48 chars.
func slugify(s string) string {
	s = strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(s), "-"), "-")
	if len(s) > 48 {
		s = strings.Trim(s[:48], "-")
	}
	return s
}

// fallbackSlug names an entity whose name has no ASCII letters/digits (e.g. "日本").
func fallbackSlug(prefix string) string {
	b := make([]byte, 3)
	_, _ = rand.Read(b)
	return prefix + "-" + hex.EncodeToString(b)
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

var errSlugTaken = fiber.NewError(fiber.StatusConflict, "slug already taken")

// RequireOrgRole gates a route on the caller's role in :orgID. Non-members
// get 404 so org existence is not leaked.
func (h *Handler) RequireOrgRole(min auth.Role) fiber.Handler {
	return func(c *fiber.Ctx) error {
		orgID, err := strconv.ParseInt(c.Params("orgID"), 10, 64)
		if err != nil {
			return errOrgNotFound
		}
		ctx, cancel := context.WithTimeout(c.Context(), 5*time.Second)
		defer cancel()
		var role string
		err = h.db.QueryRow(ctx, `SELECT role FROM organization_members WHERE org_id = $1 AND user_id = $2`,
			orgID, userID(c)).Scan(&role)
		if errors.Is(err, pgx.ErrNoRows) {
			return errOrgNotFound
		}
		if err != nil {
			return err
		}
		if !auth.Role(role).AtLeast(min) {
			return fiber.NewError(fiber.StatusForbidden, "insufficient role")
		}
		c.Locals("orgID", orgID)
		c.Locals("orgRole", auth.Role(role))
		return c.Next()
	}
}

var errOrgNotFound = fiber.NewError(fiber.StatusNotFound, "organization not found")

type Organization struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Slug      string    `json:"slug"`
	CreatedAt time.Time `json:"created_at"`
	Role      string    `json:"role"`
}

func (h *Handler) CreateOrg(c *fiber.Ctx) error {
	var in struct {
		Name string `json:"name"`
	}
	if err := c.BodyParser(&in); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid json body")
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return fiber.NewError(fiber.StatusBadRequest, "name is required")
	}
	base := slugify(name)
	if base == "" {
		base = fallbackSlug("org")
	}
	if len(base) > 44 {
		base = strings.Trim(base[:44], "-") // leave room for a "-N" suffix
	}

	ctx, cancel := context.WithTimeout(c.Context(), 5*time.Second)
	defer cancel()
	tx, err := h.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// Org slugs are global, so a taken slug is not an error for the caller:
	// take the next free "<base>-N" (ON CONFLICT keeps the transaction usable).
	o := Organization{Role: string(auth.RoleOwner)}
	inserted := false
	for i := 1; i <= 50 && !inserted; i++ {
		slug := base
		if i > 1 {
			slug = fmt.Sprintf("%s-%d", base, i)
		}
		err = tx.QueryRow(ctx, `INSERT INTO organizations(name, slug) VALUES ($1, $2)
			ON CONFLICT (slug) DO NOTHING RETURNING id, name, slug, created_at`,
			name, slug).Scan(&o.ID, &o.Name, &o.Slug, &o.CreatedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		inserted = true
	}
	if !inserted {
		return errSlugTaken
	}
	if _, err := tx.Exec(ctx, `INSERT INTO organization_members(org_id, user_id, role) VALUES ($1, $2, 'owner')`, o.ID, userID(c)); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(o)
}

func (h *Handler) ListOrgs(c *fiber.Ctx) error {
	ctx, cancel := context.WithTimeout(c.Context(), 5*time.Second)
	defer cancel()
	rows, err := h.db.Query(ctx, `
		SELECT o.id, o.name, o.slug, o.created_at, m.role FROM organization_members m
		JOIN organizations o ON o.id = m.org_id WHERE m.user_id = $1 ORDER BY o.name`, userID(c))
	if err != nil {
		return err
	}
	defer rows.Close()
	items := []Organization{}
	for rows.Next() {
		var o Organization
		if err := rows.Scan(&o.ID, &o.Name, &o.Slug, &o.CreatedAt, &o.Role); err != nil {
			return err
		}
		items = append(items, o)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return c.JSON(fiber.Map{"items": items})
}
