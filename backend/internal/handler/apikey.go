package handler

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5"

	"github.com/tiofani03/artracker/backend/internal/auth"
)

// APIKey is key metadata; the hash and plaintext are never part of it.
type APIKey struct {
	ID        int64      `json:"id"`
	ProjectID int64      `json:"project_id"`
	KeyType   string     `json:"key_type"`
	Prefix    string     `json:"prefix"`
	IsActive  bool       `json:"is_active"`
	CreatedAt time.Time  `json:"created_at"`
	RevokedAt *time.Time `json:"revoked_at"`
}

const keyColumns = `id, project_id, key_type, prefix, is_active, created_at, revoked_at`

func scanKey(row pgx.Row, k *APIKey) error {
	return row.Scan(&k.ID, &k.ProjectID, &k.KeyType, &k.Prefix, &k.IsActive, &k.CreatedAt, &k.RevokedAt)
}

var errKeyNotFound = fiber.NewError(fiber.StatusNotFound, "key not found")

// RequireProjectRole gates a route on the caller's role in the org owning
// :projectID. Non-members (and unknown projects) get 404.
func (h *Handler) RequireProjectRole(min auth.Role) fiber.Handler {
	return func(c *fiber.Ctx) error {
		pid, err := strconv.ParseInt(c.Params("projectID"), 10, 64)
		if err != nil {
			return errProjectNotFound
		}
		if h.demoMode(c) {
			c.Locals("projectID", pid)
			return c.Next()
		}
		ctx, cancel := context.WithTimeout(c.Context(), 5*time.Second)
		defer cancel()
		role, err := h.projectRole(ctx, userID(c), pid)
		if errors.Is(err, pgx.ErrNoRows) {
			return errProjectNotFound
		}
		if err != nil {
			return err
		}
		if !role.AtLeast(min) {
			return fiber.NewError(fiber.StatusForbidden, "insufficient role")
		}
		c.Locals("projectID", pid)
		return c.Next()
	}
}

func (h *Handler) ListKeys(c *fiber.Ctx) error {
	ctx, cancel := context.WithTimeout(c.Context(), 5*time.Second)
	defer cancel()
	rows, err := h.db.Query(ctx, `SELECT `+keyColumns+` FROM api_keys WHERE project_id = $1 ORDER BY created_at DESC, id DESC`,
		c.Locals("projectID"))
	if err != nil {
		return err
	}
	defer rows.Close()
	items := []APIKey{}
	for rows.Next() {
		var k APIKey
		if err := scanKey(rows, &k); err != nil {
			return err
		}
		items = append(items, k)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return c.JSON(fiber.Map{"items": items})
}

type dbQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// insertKey generates and stores a key, returning metadata and the plaintext.
func insertKey(ctx context.Context, q dbQuerier, projectID int64, kt auth.KeyType) (APIKey, string, error) {
	plain, hash, prefix, err := auth.GenerateKey(kt)
	if err != nil {
		return APIKey{}, "", err
	}
	var k APIKey
	err = scanKey(q.QueryRow(ctx,
		`INSERT INTO api_keys(project_id, key_type, key_hash, prefix) VALUES ($1, $2, $3, $4) RETURNING `+keyColumns,
		projectID, string(kt), hash, prefix), &k)
	return k, plain, err
}

func (h *Handler) CreateKey(c *fiber.Ctx) error {
	var in struct {
		KeyType string `json:"key_type"`
	}
	if err := c.BodyParser(&in); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid json body")
	}
	kt := auth.KeyType(in.KeyType)
	if kt != auth.KeyPublicClient && kt != auth.KeySecretAdmin {
		return fiber.NewError(fiber.StatusBadRequest, "key_type must be public_client or secret_admin")
	}
	ctx, cancel := context.WithTimeout(c.Context(), 5*time.Second)
	defer cancel()
	k, plain, err := insertKey(ctx, h.db, c.Locals("projectID").(int64), kt)
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"key": k, "secret": plain})
}

func (h *Handler) RevokeKey(c *fiber.Ctx) error {
	keyID, err := strconv.ParseInt(c.Params("keyID"), 10, 64)
	if err != nil {
		return errKeyNotFound
	}
	ctx, cancel := context.WithTimeout(c.Context(), 5*time.Second)
	defer cancel()
	var k APIKey
	// COALESCE keeps the original revoked_at, which makes revoke idempotent.
	err = scanKey(h.db.QueryRow(ctx, `
		UPDATE api_keys SET is_active = false, revoked_at = COALESCE(revoked_at, now())
		WHERE id = $1 AND project_id = $2 RETURNING `+keyColumns, keyID, c.Locals("projectID")), &k)
	if errors.Is(err, pgx.ErrNoRows) {
		return errKeyNotFound
	}
	if err != nil {
		return err
	}
	if h.ingestSvc != nil {
		h.ingestSvc.KeyCache().InvalidateAll()
	}
	return c.JSON(k)
}

// RotateKey atomically revokes a key and issues a new one of the same type.
func (h *Handler) RotateKey(c *fiber.Ctx) error {
	keyID, err := strconv.ParseInt(c.Params("keyID"), 10, 64)
	if err != nil {
		return errKeyNotFound
	}
	projectID := c.Locals("projectID").(int64)
	ctx, cancel := context.WithTimeout(c.Context(), 5*time.Second)
	defer cancel()
	tx, err := h.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var keyType string
	var active bool
	err = tx.QueryRow(ctx, `SELECT key_type, is_active FROM api_keys WHERE id = $1 AND project_id = $2 FOR UPDATE`,
		keyID, projectID).Scan(&keyType, &active)
	if errors.Is(err, pgx.ErrNoRows) {
		return errKeyNotFound
	}
	if err != nil {
		return err
	}
	if !active {
		return fiber.NewError(fiber.StatusConflict, "key is already revoked")
	}
	if _, err := tx.Exec(ctx, `UPDATE api_keys SET is_active = false, revoked_at = now() WHERE id = $1`, keyID); err != nil {
		return err
	}
	k, plain, err := insertKey(ctx, tx, projectID, auth.KeyType(keyType))
	if err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	if h.ingestSvc != nil {
		h.ingestSvc.KeyCache().InvalidateAll()
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"key": k, "secret": plain})
}

// ValidateKey resolves a plaintext key to its project; only active keys match.
func (h *Handler) ValidateKey(ctx context.Context, plain string) (int64, auth.KeyType, bool) {
	if plain == "" {
		return 0, "", false
	}
	if h.ingestSvc != nil {
		info, err := h.ingestSvc.KeyCache().Resolve(ctx, plain)
		if err == nil {
			return info.ProjectID, info.KeyType, true
		}
		return 0, "", false
	}
	var pid int64
	var kt string
	err := h.db.QueryRow(ctx, `SELECT project_id, key_type FROM api_keys WHERE key_hash = $1 AND is_active`,
		auth.HashKey(plain)).Scan(&pid, &kt)
	if err != nil {
		return 0, "", false
	}
	return pid, auth.KeyType(kt), true
}

func (h *Handler) sdkValidate(adminOnly bool) fiber.Handler {
	return func(c *fiber.Ctx) error {
		ctx, cancel := context.WithTimeout(c.Context(), 5*time.Second)
		defer cancel()
		pid, kt, ok := h.ValidateKey(ctx, c.Get("X-Artracker-Key"))
		if !ok {
			return fiber.NewError(fiber.StatusUnauthorized, "invalid api key")
		}
		if adminOnly && kt != auth.KeySecretAdmin {
			return fiber.NewError(fiber.StatusForbidden, "secret key required")
		}
		return c.JSON(fiber.Map{"project_id": pid, "key_type": kt})
	}
}
