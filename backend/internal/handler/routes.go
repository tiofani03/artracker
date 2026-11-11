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

	api.Post("/ingest", h.VerifyIngestKey, h.RateLimitIngest, h.Ingest)
	api.Post("/events", h.VerifyIngestKey, h.RateLimitAnalytics, h.IngestEvents)
	api.Post("/replay/chunk-url", h.VerifyIngestKey, h.RateLimitReplay, h.GenerateChunkUploadURL)
	api.Get("/replay/config", h.VerifyIngestKey, h.RateLimitReplay, h.GetReplayConfig)
	api.Put("/replay/upload-chunk", h.UploadChunkProxy)
	api.Get("/replay/download-chunk", h.DownloadChunkProxy)
	api.Post("/replay/chunk-ack", h.VerifyIngestKey, h.RateLimitReplay, h.AcknowledgeChunk)

	if h.db == nil {
		// SKIP_DB mode: only the in-memory artwork routes exist.
		off := func(*fiber.Ctx) error {
			return fiber.NewError(fiber.StatusServiceUnavailable, "this feature needs the database (SKIP_DB is enabled)")
		}
		for _, prefix := range []string{"/auth", "/orgs", "/projects", "/sdk", "/replay"} {
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
	api.Get("/orgs/:orgID/members", h.RequireAuth, h.RequireOrgRole(auth.RoleViewer), h.ListOrgMembers)
	api.Post("/orgs/:orgID/invites", h.RequireAuth, h.RequireOrgRole(auth.RoleAdmin), h.CreateOrgInvite)
	api.Delete("/orgs/:orgID/invites/:inviteID", h.RequireAuth, h.RequireOrgRole(auth.RoleAdmin), h.RevokeOrgInvite)
	api.Patch("/orgs/:orgID/members/:userID", h.RequireAuth, h.RequireOrgRole(auth.RoleOwner), h.UpdateOrgMemberRole)
	api.Delete("/orgs/:orgID/members/:userID", h.RequireAuth, h.RequireOrgRole(auth.RoleAdmin), h.RemoveOrgMember)

	// Public invitation acceptance
	api.Get("/invites/:token", h.GetInvitePreview)
	api.Post("/invites/:token/accept", h.RequireAuth, h.AcceptInvite)

	api.Get("/projects/:projectID", h.RequireAuth, h.GetProject)
	api.Patch("/projects/:projectID/replay-settings", h.RequireAuth, h.RequireProjectRole(auth.RoleAdmin), h.UpdateReplaySettings)

	api.Get("/projects/:projectID/keys", h.RequireAuth, h.RequireProjectRole(auth.RoleViewer), h.ListKeys)
	api.Post("/projects/:projectID/keys", h.RequireAuth, h.RequireProjectRole(auth.RoleAdmin), h.CreateKey)
	api.Post("/projects/:projectID/keys/:keyID/revoke", h.RequireAuth, h.RequireProjectRole(auth.RoleAdmin), h.RevokeKey)
	api.Post("/projects/:projectID/keys/:keyID/rotate", h.RequireAuth, h.RequireProjectRole(auth.RoleAdmin), h.RotateKey)

	api.Get("/projects/:projectID/stats", h.RequireAuth, h.RequireProjectRole(auth.RoleViewer), h.GetProjectStats)
	api.Get("/projects/:projectID/events", h.RequireAuth, h.RequireProjectRole(auth.RoleViewer), h.ListProjectEvents)
	api.Get("/projects/:projectID/sessions", h.RequireAuth, h.RequireProjectRole(auth.RoleViewer), h.ListProjectSessions)
	api.Get("/projects/:projectID/sessions/:sessionID", h.RequireAuth, h.RequireProjectRole(auth.RoleViewer), h.GetProjectSession)
	api.Get("/projects/:projectID/sessions/:sessionID/chunks", h.RequireAuth, h.RequireProjectRole(auth.RoleViewer), h.ListSessionChunks)
	api.Get("/projects/:projectID/sessions/:sessionID/perf", h.RequireAuth, h.RequireProjectRole(auth.RoleViewer), h.GetSessionPerf)
	api.Post("/projects/:projectID/seed-mock", h.RequireAuth, h.RequireProjectRole(auth.RoleViewer), h.SeedProjectMockData)
	api.Post("/projects/:projectID/test-event", h.RequireAuth, h.RequireProjectRole(auth.RoleViewer), h.SendProjectTestEvent)

	// Growth Analytics (Funnel & Cohort Retention)
	api.Get("/projects/:projectID/analytics/catalog", h.RequireAuth, h.RequireProjectRole(auth.RoleViewer), h.GetAnalyticsCatalog)
	api.Post("/projects/:projectID/analytics/funnel", h.RequireAuth, h.RequireProjectRole(auth.RoleViewer), h.CalculateFunnel)
	api.Get("/projects/:projectID/analytics/retention", h.RequireAuth, h.RequireProjectRole(auth.RoleViewer), h.CalculateCohort)
	api.Get("/projects/:projectID/analytics/funnels", h.RequireAuth, h.RequireProjectRole(auth.RoleViewer), h.ListSavedFunnels)
	api.Post("/projects/:projectID/analytics/funnels", h.RequireAuth, h.RequireProjectRole(auth.RoleMember), h.CreateSavedFunnel)
	api.Delete("/projects/:projectID/analytics/funnels/:funnelID", h.RequireAuth, h.RequireProjectRole(auth.RoleMember), h.DeleteSavedFunnel)

	// Error tracking & crash diagnostics & release artifacts
	api.Get("/projects/:projectID/nav-counts", h.RequireAuth, h.RequireProjectRole(auth.RoleViewer), h.GetNavCounts)
	api.Get("/projects/:projectID/issues", h.RequireAuth, h.RequireProjectRole(auth.RoleViewer), h.ListIssues)
	api.Get("/projects/:projectID/issues/:issueID", h.RequireAuth, h.RequireProjectRole(auth.RoleViewer), h.GetIssue)
	api.Patch("/projects/:projectID/issues/:issueID", h.RequireAuth, h.RequireProjectRole(auth.RoleViewer), h.UpdateIssueStatus)
	api.Post("/projects/:projectID/issues/:issueID/assign", h.RequireAuth, h.RequireProjectRole(auth.RoleViewer), h.AssignIssue)
	api.Post("/projects/:projectID/releases/:version/sourcemaps", h.RequireAuth, h.RequireProjectRole(auth.RoleAdmin), h.UploadSourceMap)
	api.Post("/projects/:projectID/releases/:version/dsyms", h.RequireAuth, h.RequireProjectRole(auth.RoleAdmin), h.UploadDSYM)
	api.Post("/projects/:projectID/releases/:version/proguard", h.RequireAuth, h.RequireProjectRole(auth.RoleAdmin), h.UploadProguard)
	api.Get("/projects/:projectID/releases/:version/artifacts", h.RequireAuth, h.RequireProjectRole(auth.RoleViewer), h.ListReleaseArtifacts)

	api.Get("/sdk/validate", h.sdkValidate(false))
	api.Get("/sdk/validate-admin", h.sdkValidate(true))

	api.Get("/openapi.json", h.OpenAPISpec)
}
