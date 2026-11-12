package handler

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5"

	"github.com/tiofani03/artracker/backend/internal/auth"
)

type OrganizationMemberItem struct {
	UserID    int64     `json:"user_id"`
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	AvatarURL string    `json:"avatar_url"`
	Role      string    `json:"role"`
	JoinedAt  time.Time `json:"joined_at"`
}

type OrganizationInvitationItem struct {
	ID            int64      `json:"id"`
	OrgID         int64      `json:"org_id"`
	Email         string     `json:"email"`
	Role          string     `json:"role"`
	InvitedBy     int64      `json:"invited_by"`
	InvitedByName string     `json:"invited_by_name,omitempty"`
	ExpiresAt     time.Time  `json:"expires_at"`
	AcceptedAt    *time.Time `json:"accepted_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
}

type CreateInviteRequest struct {
	Email string `json:"email"`
	Role  string `json:"role"`
}

type CreateInviteResponse struct {
	ID        int64     `json:"id"`
	OrgID     int64     `json:"org_id"`
	Email     string    `json:"email"`
	Role      string    `json:"role"`
	InviteURL string    `json:"invite_url"`
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

type InvitePreviewResponse struct {
	OrgID         int64     `json:"org_id"`
	OrgName       string    `json:"org_name"`
	OrgSlug       string    `json:"org_slug"`
	Email         string    `json:"email"`
	Role          string    `json:"role"`
	InvitedByName string    `json:"invited_by_name"`
	ExpiresAt     time.Time `json:"expires_at"`
	IsExpired     bool      `json:"is_expired"`
	IsAccepted    bool      `json:"is_accepted"`
}

type AcceptInviteResponse struct {
	OrgID   int64  `json:"org_id"`
	OrgName string `json:"org_name"`
	OrgSlug string `json:"org_slug"`
	Role    string `json:"role"`
}

type UpdateMemberRoleRequest struct {
	Role string `json:"role"`
}

func hashInviteToken(plain string) string {
	sum := sha256.Sum256([]byte(plain))
	return hex.EncodeToString(sum[:])
}

// ListOrgMembers handles GET /api/v1/orgs/:orgID/members
func (h *Handler) ListOrgMembers(c *fiber.Ctx) error {
	orgID := c.Locals("orgID").(int64)

	ctx, cancel := context.WithTimeout(c.Context(), 5*time.Second)
	defer cancel()

	if h.db == nil {
		return c.JSON(fiber.Map{
			"members":     []OrganizationMemberItem{},
			"invitations": []OrganizationInvitationItem{},
		})
	}

	// 1. Fetch active members
	rows, err := h.db.Query(ctx, `
		SELECT u.id, u.email, u.name, u.avatar_url, m.role, u.created_at
		FROM organization_members m
		JOIN users u ON u.id = m.user_id
		WHERE m.org_id = $1
		ORDER BY CASE m.role WHEN 'owner' THEN 1 WHEN 'admin' THEN 2 WHEN 'member' THEN 3 ELSE 4 END, u.name ASC`, orgID)
	if err != nil {
		return fmt.Errorf("list org members: %w", err)
	}
	defer rows.Close()

	members := make([]OrganizationMemberItem, 0)
	for rows.Next() {
		var m OrganizationMemberItem
		if err := rows.Scan(&m.UserID, &m.Email, &m.Name, &m.AvatarURL, &m.Role, &m.JoinedAt); err != nil {
			return fmt.Errorf("scan member: %w", err)
		}
		members = append(members, m)
	}

	// 2. Fetch pending invitations
	invRows, err := h.db.Query(ctx, `
		SELECT i.id, i.org_id, i.email, i.role, i.invited_by, COALESCE(u.name, u.email), i.expires_at, i.created_at
		FROM organization_invitations i
		JOIN users u ON u.id = i.invited_by
		WHERE i.org_id = $1 AND i.accepted_at IS NULL AND i.expires_at > now()
		ORDER BY i.created_at DESC`, orgID)
	if err != nil {
		return fmt.Errorf("list org invitations: %w", err)
	}
	defer invRows.Close()

	invitations := make([]OrganizationInvitationItem, 0)
	for invRows.Next() {
		var inv OrganizationInvitationItem
		if err := invRows.Scan(&inv.ID, &inv.OrgID, &inv.Email, &inv.Role, &inv.InvitedBy, &inv.InvitedByName, &inv.ExpiresAt, &inv.CreatedAt); err != nil {
			return fmt.Errorf("scan invite: %w", err)
		}
		invitations = append(invitations, inv)
	}

	return c.JSON(fiber.Map{
		"members":     members,
		"invitations": invitations,
	})
}

// CreateOrgInvite handles POST /api/v1/orgs/:orgID/invites
func (h *Handler) CreateOrgInvite(c *fiber.Ctx) error {
	orgID := c.Locals("orgID").(int64)
	callerID := userID(c)

	var req CreateInviteRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid json body")
	}

	cleanEmail := strings.ToLower(strings.TrimSpace(req.Email))
	if _, err := mail.ParseAddress(cleanEmail); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "valid email address is required")
	}

	targetRole := strings.ToLower(strings.TrimSpace(req.Role))
	if targetRole != "admin" && targetRole != "member" && targetRole != "viewer" {
		return fiber.NewError(fiber.StatusBadRequest, "role must be admin, member, or viewer")
	}

	ctx, cancel := context.WithTimeout(c.Context(), 5*time.Second)
	defer cancel()

	if h.db == nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, "database is disabled")
	}

	// Check if already an active member
	var alreadyMember bool
	err := h.db.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM organization_members m
			JOIN users u ON u.id = m.user_id
			WHERE m.org_id = $1 AND lower(u.email) = $2
		)`, orgID, cleanEmail).Scan(&alreadyMember)
	if err != nil {
		return fmt.Errorf("check membership: %w", err)
	}
	if alreadyMember {
		return fiber.NewError(fiber.StatusConflict, "this user is already a member of this organization")
	}

	// Generate 32-byte secure token
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return fmt.Errorf("generate token: %w", err)
	}
	rawToken := hex.EncodeToString(tokenBytes)
	tokenHash := hashInviteToken(rawToken)
	expiresAt := time.Now().Add(7 * 24 * time.Hour)

	var inviteID int64
	err = h.db.QueryRow(ctx, `
		INSERT INTO organization_invitations (
			org_id, email, role, token_hash, invited_by, expires_at
		) VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id`, orgID, cleanEmail, targetRole, tokenHash, callerID, expiresAt).Scan(&inviteID)
	if err != nil {
		return fmt.Errorf("insert invitation: %w", err)
	}

	inviteURL := fmt.Sprintf("%s/invite/%s", strings.TrimRight(h.cfg.PublicURL, "/"), rawToken)

	return c.Status(fiber.StatusCreated).JSON(CreateInviteResponse{
		ID:        inviteID,
		OrgID:     orgID,
		Email:     cleanEmail,
		Role:      targetRole,
		InviteURL: inviteURL,
		Token:     rawToken,
		ExpiresAt: expiresAt,
	})
}

// RevokeOrgInvite handles DELETE /api/v1/orgs/:orgID/invites/:inviteID
func (h *Handler) RevokeOrgInvite(c *fiber.Ctx) error {
	orgID := c.Locals("orgID").(int64)
	inviteID, err := c.ParamsInt("inviteID")
	if err != nil || inviteID <= 0 {
		return fiber.NewError(fiber.StatusBadRequest, "invalid invite id")
	}

	ctx, cancel := context.WithTimeout(c.Context(), 5*time.Second)
	defer cancel()

	if h.db == nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, "database is disabled")
	}

	tag, err := h.db.Exec(ctx, `
		DELETE FROM organization_invitations
		WHERE id = $1 AND org_id = $2 AND accepted_at IS NULL`, inviteID, orgID)
	if err != nil {
		return fmt.Errorf("delete invitation: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fiber.NewError(fiber.StatusNotFound, "invitation not found or already accepted")
	}

	return c.SendStatus(fiber.StatusNoContent)
}

// UpdateOrgMemberRole handles PATCH /api/v1/orgs/:orgID/members/:userID
func (h *Handler) UpdateOrgMemberRole(c *fiber.Ctx) error {
	orgID := c.Locals("orgID").(int64)
	targetUserID, err := c.ParamsInt("userID")
	if err != nil || targetUserID <= 0 {
		return fiber.NewError(fiber.StatusBadRequest, "invalid user id")
	}

	callerRole, _ := c.Locals("orgRole").(auth.Role)
	if callerRole != auth.RoleOwner {
		return fiber.NewError(fiber.StatusForbidden, "only organization owners can change member roles")
	}

	var req UpdateMemberRoleRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid json body")
	}
	newRole := strings.ToLower(strings.TrimSpace(req.Role))
	if newRole != "admin" && newRole != "member" && newRole != "viewer" {
		return fiber.NewError(fiber.StatusBadRequest, "role must be admin, member, or viewer")
	}

	ctx, cancel := context.WithTimeout(c.Context(), 5*time.Second)
	defer cancel()

	if h.db == nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, "database is disabled")
	}

	// Prevent demoting the owner via this endpoint
	var currentRole string
	err = h.db.QueryRow(ctx, `SELECT role FROM organization_members WHERE org_id = $1 AND user_id = $2`, orgID, targetUserID).Scan(&currentRole)
	if errors.Is(err, pgx.ErrNoRows) {
		return fiber.NewError(fiber.StatusNotFound, "member not found")
	}
	if err != nil {
		return fmt.Errorf("query member: %w", err)
	}
	if currentRole == "owner" {
		return fiber.NewError(fiber.StatusBadRequest, "cannot change the role of the organization owner")
	}

	tx, err := h.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	_, err = tx.Exec(ctx, `
		UPDATE organization_members
		SET role = $1
		WHERE org_id = $2 AND user_id = $3`, newRole, orgID, targetUserID)
	if err != nil {
		return fmt.Errorf("update member role: %w", err)
	}
	// A viewer cannot mint mcp keys, so a demoted member keeps none.
	var revoked int64
	if newRole == "viewer" {
		if revoked, err = revokeMemberMCPKeys(ctx, tx, orgID, int64(targetUserID)); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	if revoked > 0 {
		h.invalidateKeyCache()
	}

	return c.JSON(fiber.Map{
		"org_id":  orgID,
		"user_id": targetUserID,
		"role":    newRole,
	})
}

// RemoveOrgMember handles DELETE /api/v1/orgs/:orgID/members/:userID
func (h *Handler) RemoveOrgMember(c *fiber.Ctx) error {
	orgID := c.Locals("orgID").(int64)
	targetUserID, err := c.ParamsInt("userID")
	if err != nil || targetUserID <= 0 {
		return fiber.NewError(fiber.StatusBadRequest, "invalid user id")
	}

	ctx, cancel := context.WithTimeout(c.Context(), 5*time.Second)
	defer cancel()

	if h.db == nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, "database is disabled")
	}

	var targetRole string
	err = h.db.QueryRow(ctx, `SELECT role FROM organization_members WHERE org_id = $1 AND user_id = $2`, orgID, targetUserID).Scan(&targetRole)
	if errors.Is(err, pgx.ErrNoRows) {
		return fiber.NewError(fiber.StatusNotFound, "member not found")
	}
	if err != nil {
		return fmt.Errorf("query member: %w", err)
	}
	if targetRole == "owner" {
		return fiber.NewError(fiber.StatusBadRequest, "cannot remove the organization owner")
	}

	tx, err := h.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	_, err = tx.Exec(ctx, `DELETE FROM organization_members WHERE org_id = $1 AND user_id = $2`, orgID, targetUserID)
	if err != nil {
		return fmt.Errorf("delete member: %w", err)
	}
	// Removal ends the member's MCP access: their mcp keys go with them.
	revoked, err := revokeMemberMCPKeys(ctx, tx, orgID, int64(targetUserID))
	if err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	if revoked > 0 {
		h.invalidateKeyCache()
	}

	return c.SendStatus(fiber.StatusNoContent)
}

// GetInvitePreview handles GET /api/v1/invites/:token (Public endpoint)
func (h *Handler) GetInvitePreview(c *fiber.Ctx) error {
	rawToken := strings.TrimSpace(c.Params("token"))
	if len(rawToken) < 16 {
		return fiber.NewError(fiber.StatusBadRequest, "invalid invitation token")
	}

	ctx, cancel := context.WithTimeout(c.Context(), 5*time.Second)
	defer cancel()

	if h.db == nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, "database is disabled")
	}

	tokenHash := hashInviteToken(rawToken)

	var p InvitePreviewResponse
	var acceptedAt *time.Time
	err := h.db.QueryRow(ctx, `
		SELECT
			i.org_id, o.name, o.slug, i.email, i.role,
			COALESCE(u.name, u.email), i.expires_at, i.accepted_at
		FROM organization_invitations i
		JOIN organizations o ON o.id = i.org_id
		JOIN users u ON u.id = i.invited_by
		WHERE i.token_hash = $1`, tokenHash).Scan(
		&p.OrgID, &p.OrgName, &p.OrgSlug, &p.Email, &p.Role,
		&p.InvitedByName, &p.ExpiresAt, &acceptedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return fiber.NewError(fiber.StatusNotFound, "invitation not found")
	}
	if err != nil {
		return fmt.Errorf("query invite: %w", err)
	}

	p.IsAccepted = acceptedAt != nil
	p.IsExpired = time.Now().After(p.ExpiresAt)

	return c.JSON(p)
}

// AcceptInvite handles POST /api/v1/invites/:token/accept (Requires Auth)
func (h *Handler) AcceptInvite(c *fiber.Ctx) error {
	rawToken := strings.TrimSpace(c.Params("token"))
	if len(rawToken) < 16 {
		return fiber.NewError(fiber.StatusBadRequest, "invalid invitation token")
	}

	currentUserID := userID(c)
	if currentUserID <= 0 {
		return fiber.NewError(fiber.StatusUnauthorized, "unauthorized")
	}

	ctx, cancel := context.WithTimeout(c.Context(), 5*time.Second)
	defer cancel()

	if h.db == nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, "database is disabled")
	}

	tokenHash := hashInviteToken(rawToken)

	tx, err := h.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var inviteID, orgID int64
	var role, orgName, orgSlug string
	var expiresAt time.Time
	var acceptedAt *time.Time

	err = tx.QueryRow(ctx, `
		SELECT i.id, i.org_id, i.role, o.name, o.slug, i.expires_at, i.accepted_at
		FROM organization_invitations i
		JOIN organizations o ON o.id = i.org_id
		WHERE i.token_hash = $1
		FOR UPDATE OF i`, tokenHash).Scan(
		&inviteID, &orgID, &role, &orgName, &orgSlug, &expiresAt, &acceptedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return fiber.NewError(fiber.StatusNotFound, "invitation not found")
	}
	if err != nil {
		return fmt.Errorf("query invite for accept: %w", err)
	}

	if acceptedAt != nil {
		return fiber.NewError(fiber.StatusConflict, "this invitation has already been accepted")
	}
	if time.Now().After(expiresAt) {
		return fiber.NewError(fiber.StatusGone, "this invitation has expired")
	}

	// Insert or update member role in organization_members
	_, err = tx.Exec(ctx, `
		INSERT INTO organization_members (org_id, user_id, role)
		VALUES ($1, $2, $3)
		ON CONFLICT (org_id, user_id) DO UPDATE SET role = EXCLUDED.role`,
		orgID, currentUserID, role)
	if err != nil {
		return fmt.Errorf("insert org membership: %w", err)
	}
	// Re-accepting as a viewer demotes an existing member: drop their mcp keys.
	var revoked int64
	if role == "viewer" {
		if revoked, err = revokeMemberMCPKeys(ctx, tx, orgID, currentUserID); err != nil {
			return err
		}
	}

	// Mark invitation accepted
	_, err = tx.Exec(ctx, `
		UPDATE organization_invitations
		SET accepted_at = now(), accepted_by = $1
		WHERE id = $2`, currentUserID, inviteID)
	if err != nil {
		return fmt.Errorf("mark invite accepted: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return err
	}
	if revoked > 0 {
		h.invalidateKeyCache()
	}

	return c.JSON(AcceptInviteResponse{
		OrgID:   orgID,
		OrgName: orgName,
		OrgSlug: orgSlug,
		Role:    role,
	})
}
