package handler

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/tiofani03/artracker/backend/internal/people"
)

const (
	peopleSearchMaxLen       = 128
	personSessionsLimit      = 50
	sessionOtherSessionLimit = 10
)

// PeopleStore returns the end-user profile store; nil (whose methods no-op) when the database is disabled.
func (h *Handler) PeopleStore() *people.Store {
	if h.db == nil {
		return nil
	}
	return people.NewStore(h.db)
}

// PeopleListResponse is the body of GET /projects/:projectID/people.
type PeopleListResponse struct {
	Items  []people.Person `json:"items"`
	Total  int             `json:"total"`
	Limit  int             `json:"limit"`
	Offset int             `json:"offset"`
}

// PersonDetailResponse is the body of GET /projects/:projectID/people/:userID.
type PersonDetailResponse struct {
	Person   people.Person          `json:"person"`
	Sessions []people.PersonSession `json:"sessions"`
}

// SessionContextResponse is the body of GET /projects/:projectID/sessions/:sessionID/context.
type SessionContextResponse struct {
	SessionID     string                 `json:"session_id"`
	UserID        string                 `json:"user_id"`
	Context       map[string]any         `json:"context"`
	Person        *people.Person         `json:"person"`
	OtherSessions []people.PersonSession `json:"other_sessions"`
}

func pathParam(c *fiber.Ctx, name string) (string, error) {
	v, err := url.PathUnescape(c.Params(name))
	if err != nil {
		return "", fiber.NewError(fiber.StatusBadRequest, "invalid "+name)
	}
	return v, nil
}

// ListPeople handles GET /api/v1/projects/:projectID/people.
func (h *Handler) ListPeople(c *fiber.Ctx) error {
	projectID := c.Locals("projectID").(int64)
	limit := c.QueryInt("limit", 25)
	if limit <= 0 {
		limit = 25
	}
	if limit > 100 {
		limit = 100
	}
	offset := c.QueryInt("offset", 0)
	if offset < 0 {
		offset = 0
	}
	search := strings.TrimSpace(c.Query("search"))
	if r := []rune(search); len(r) > peopleSearchMaxLen {
		search = string(r[:peopleSearchMaxLen])
	}
	resp := PeopleListResponse{Items: []people.Person{}, Limit: limit, Offset: offset}
	if st := h.PeopleStore(); st != nil {
		items, total, err := st.ListPeople(c.UserContext(), projectID, search, limit, offset)
		if err != nil {
			return fmt.Errorf("list people: %w", err)
		}
		resp.Items, resp.Total = items, total
	}
	return c.JSON(resp)
}

// GetPerson handles GET /api/v1/projects/:projectID/people/:userID.
func (h *Handler) GetPerson(c *fiber.Ctx) error {
	projectID := c.Locals("projectID").(int64)
	userID, err := pathParam(c, "userID")
	if err != nil {
		return err
	}
	st := h.PeopleStore()
	if st == nil || userID == "" {
		return fiber.NewError(fiber.StatusNotFound, "person not found")
	}
	ctx := c.UserContext()
	p, err := st.GetPerson(ctx, projectID, userID)
	if err != nil {
		return fmt.Errorf("get person: %w", err)
	}
	if p == nil {
		return fiber.NewError(fiber.StatusNotFound, "person not found")
	}
	sessions, err := st.PersonSessions(ctx, projectID, userID, "", personSessionsLimit)
	if err != nil {
		return fmt.Errorf("person sessions: %w", err)
	}
	return c.JSON(PersonDetailResponse{Person: *p, Sessions: sessions})
}

// GetSessionContext handles GET /api/v1/projects/:projectID/sessions/:sessionID/context: the user and device
// context of one session, the user's profile and their other recent sessions. Unknown sessions answer 200 with
// empty fields so the replay User tab can always render.
func (h *Handler) GetSessionContext(c *fiber.Ctx) error {
	projectID := c.Locals("projectID").(int64)
	sessionID, err := pathParam(c, "sessionID")
	if err != nil {
		return err
	}
	resp := SessionContextResponse{SessionID: sessionID, Context: map[string]any{}, OtherSessions: []people.PersonSession{}}
	st := h.PeopleStore()
	if st == nil {
		return c.JSON(resp)
	}
	ctx := c.UserContext()
	userID, sessCtx, err := st.SessionContext(ctx, projectID, sessionID)
	if err != nil {
		return fmt.Errorf("session context: %w", err)
	}
	resp.UserID, resp.Context = userID, sessCtx
	if userID != "" {
		if resp.Person, err = st.GetPerson(ctx, projectID, userID); err != nil {
			return fmt.Errorf("session person: %w", err)
		}
		if resp.OtherSessions, err = st.PersonSessions(ctx, projectID, userID, sessionID, sessionOtherSessionLimit); err != nil {
			return fmt.Errorf("other sessions: %w", err)
		}
	}
	return c.JSON(resp)
}
