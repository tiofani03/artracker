package handler

import (
	"github.com/gofiber/fiber/v2"

	"github.com/tiofani03/artracker/backend/internal/auth"
)

// Routes registers every /api/v1 route. Shared by main and tests.
func (h *Handler) Routes(api fiber.Router) {
	api.Get("/artworks", h.ListArtworks)
	api.Post("/artworks", h.CreateArtwork)
	api.Get("/artworks/:id", h.GetArtwork)
	api.Patch("/artworks/:id", h.UpdateArtwork)
	api.Delete("/artworks/:id", h.DeleteArtwork)

	if h.db == nil {
		// SKIP_DB mode: only the in-memory artwork routes exist.
		off := func(*fiber.Ctx) error {
			return fiber.NewError(fiber.StatusServiceUnavailable, "this feature needs the database (SKIP_DB is enabled)")
		}
		for _, prefix := range []string{"/auth", "/orgs", "/projects", "/sdk"} {
			api.All(prefix, off)
			api.All(prefix+"/*", off)
		}
		return
	}

	a := api.Group("/auth")
	a.Get("/:provider/login", h.OAuthLogin)
	a.Get("/:provider/callback", h.OAuthCallback)
	a.Post("/refresh", h.Refresh)
	a.Post("/logout", h.Logout)
	a.Get("/me", h.RequireAuth, h.Me)

	api.Post("/orgs", h.RequireAuth, h.CreateOrg)
	api.Get("/orgs", h.RequireAuth, h.ListOrgs)
	api.Get("/orgs/:orgID/projects", h.RequireAuth, h.RequireOrgRole(auth.RoleViewer), h.ListProjects)
	api.Post("/orgs/:orgID/projects", h.RequireAuth, h.RequireOrgRole(auth.RoleAdmin), h.CreateProject)
	api.Get("/projects/:projectID", h.RequireAuth, h.GetProject)

	api.Get("/projects/:projectID/keys", h.RequireAuth, h.RequireProjectRole(auth.RoleViewer), h.ListKeys)
	api.Post("/projects/:projectID/keys", h.RequireAuth, h.RequireProjectRole(auth.RoleAdmin), h.CreateKey)
	api.Post("/projects/:projectID/keys/:keyID/revoke", h.RequireAuth, h.RequireProjectRole(auth.RoleAdmin), h.RevokeKey)
	api.Post("/projects/:projectID/keys/:keyID/rotate", h.RequireAuth, h.RequireProjectRole(auth.RoleAdmin), h.RotateKey)

	api.Get("/sdk/validate", h.sdkValidate(false))
	api.Get("/sdk/validate-admin", h.sdkValidate(true))
}
