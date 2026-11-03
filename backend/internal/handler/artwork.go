package handler

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5"
)

type Artwork struct {
	ID        int64     `json:"id"`
	Title     string    `json:"title"`
	Artist    string    `json:"artist"`
	Year      *int32    `json:"year"`
	Medium    string    `json:"medium"`
	Notes     string    `json:"notes"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type artworkInput struct {
	Title  *string `json:"title"`
	Artist *string `json:"artist"`
	Year   *int32  `json:"year"`
	Medium *string `json:"medium"`
	Notes  *string `json:"notes"`
}

const artworkColumns = `id, title, artist, year, medium, notes, created_at, updated_at`

func scanArtwork(row pgx.Row, a *Artwork) error {
	return row.Scan(&a.ID, &a.Title, &a.Artist, &a.Year, &a.Medium, &a.Notes, &a.CreatedAt, &a.UpdatedAt)
}

// ListArtworks returns newest first, paginated with limit/offset.
func (h *Handler) ListArtworks(c *fiber.Ctx) error {
	limit, _ := strconv.Atoi(c.Query("limit", "50"))
	offset, _ := strconv.Atoi(c.Query("offset", "0"))
	if limit < 1 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	ctx, cancel := context.WithTimeout(c.Context(), 5*time.Second)
	defer cancel()

	rows, err := h.db.Query(ctx, `SELECT `+artworkColumns+` FROM artworks ORDER BY created_at DESC LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return err
	}
	defer rows.Close()

	items := make([]Artwork, 0, limit)
	for rows.Next() {
		var a Artwork
		if err := scanArtwork(rows, &a); err != nil {
			return err
		}
		items = append(items, a)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	return c.JSON(fiber.Map{"items": items, "limit": limit, "offset": offset})
}

func (h *Handler) GetArtwork(c *fiber.Ctx) error {
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid id")
	}

	ctx, cancel := context.WithTimeout(c.Context(), 5*time.Second)
	defer cancel()

	var a Artwork
	err = scanArtwork(h.db.QueryRow(ctx, `SELECT `+artworkColumns+` FROM artworks WHERE id = $1`, id), &a)
	if errors.Is(err, pgx.ErrNoRows) {
		return fiber.NewError(fiber.StatusNotFound, "artwork not found")
	}
	if err != nil {
		return err
	}
	return c.JSON(a)
}

func (h *Handler) CreateArtwork(c *fiber.Ctx) error {
	var in artworkInput
	if err := c.BodyParser(&in); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid json body")
	}
	title := strings.TrimSpace(deref(in.Title))
	if title == "" {
		return fiber.NewError(fiber.StatusBadRequest, "title is required")
	}
	if in.Year != nil && (*in.Year < 0 || *in.Year > 3000) {
		return fiber.NewError(fiber.StatusBadRequest, "year out of range")
	}

	ctx, cancel := context.WithTimeout(c.Context(), 5*time.Second)
	defer cancel()

	var a Artwork
	err := scanArtwork(h.db.QueryRow(ctx,
		`INSERT INTO artworks (title, artist, year, medium, notes)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING `+artworkColumns,
		title, strings.TrimSpace(deref(in.Artist)), in.Year, strings.TrimSpace(deref(in.Medium)), strings.TrimSpace(deref(in.Notes)),
	), &a)
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(a)
}

func (h *Handler) UpdateArtwork(c *fiber.Ctx) error {
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid id")
	}

	var in artworkInput
	if err := c.BodyParser(&in); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid json body")
	}
	if in.Title != nil && strings.TrimSpace(*in.Title) == "" {
		return fiber.NewError(fiber.StatusBadRequest, "title cannot be empty")
	}
	if in.Year != nil && (*in.Year < 0 || *in.Year > 3000) {
		return fiber.NewError(fiber.StatusBadRequest, "year out of range")
	}

	ctx, cancel := context.WithTimeout(c.Context(), 5*time.Second)
	defer cancel()

	var a Artwork
	err = scanArtwork(h.db.QueryRow(ctx,
		`UPDATE artworks SET
		   title  = COALESCE(NULLIF(TRIM($2), ''), title),
		   artist = COALESCE($3, artist),
		   year   = COALESCE($4, year),
		   medium = COALESCE($5, medium),
		   notes  = COALESCE($6, notes),
		   updated_at = now()
		 WHERE id = $1
		 RETURNING `+artworkColumns,
		id, in.Title, in.Artist, in.Year, in.Medium, in.Notes,
	), &a)
	if errors.Is(err, pgx.ErrNoRows) {
		return fiber.NewError(fiber.StatusNotFound, "artwork not found")
	}
	if err != nil {
		return err
	}
	return c.JSON(a)
}

func (h *Handler) DeleteArtwork(c *fiber.Ctx) error {
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid id")
	}

	ctx, cancel := context.WithTimeout(c.Context(), 5*time.Second)
	defer cancel()

	tag, err := h.db.Exec(ctx, `DELETE FROM artworks WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fiber.NewError(fiber.StatusNotFound, "artwork not found")
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
