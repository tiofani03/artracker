package handler

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func TestInvitationsLifecycle(t *testing.T) {
	e := newTestEnv(t)

	ownerID, ownerHdr := e.addUser(t, "orgowner")
	orgID := e.addOrg(t, "acme-team")
	e.addMember(t, orgID, ownerID, "owner")

	// 1. List members (initially only owner)
	resp := e.do(t, "GET", fmt.Sprintf("/api/v1/orgs/%d/members", orgID), nil, ownerHdr)
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("list members status = %d", resp.StatusCode)
	}
	var listRes struct {
		Members     []OrganizationMemberItem     `json:"members"`
		Invitations []OrganizationInvitationItem `json:"invitations"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&listRes)
	if len(listRes.Members) != 1 || listRes.Members[0].Role != "owner" {
		t.Fatalf("expected 1 owner member, got %+v", listRes.Members)
	}
	if len(listRes.Invitations) != 0 {
		t.Fatalf("expected 0 initial invitations")
	}

	// 2. Create invite for teammate
	invitePayload := `{"email":"teammate@company.com","role":"member"}`
	resp = e.post(t, fmt.Sprintf("/api/v1/orgs/%d/invites", orgID), invitePayload, ownerHdr)
	if resp.StatusCode != fiber.StatusCreated {
		t.Fatalf("create invite status = %d", resp.StatusCode)
	}
	var invRes CreateInviteResponse
	_ = json.NewDecoder(resp.Body).Decode(&invRes)
	if invRes.Token == "" || !strings.Contains(invRes.InviteURL, "/invite/") {
		t.Fatalf("invalid invite response: %+v", invRes)
	}

	// 3. Public preview without auth
	previewReq := httptest.NewRequest("GET", fmt.Sprintf("/api/v1/invites/%s", invRes.Token), nil)
	previewResp, err := e.app.Test(previewReq)
	if err != nil {
		t.Fatal(err)
	}
	if previewResp.StatusCode != fiber.StatusOK {
		t.Fatalf("preview status = %d", previewResp.StatusCode)
	}
	var previewRes InvitePreviewResponse
	_ = json.NewDecoder(previewResp.Body).Decode(&previewRes)
	if previewRes.OrgName != "acme-team" || previewRes.Role != "member" || previewRes.IsAccepted || previewRes.IsExpired {
		t.Fatalf("unexpected preview data: %+v", previewRes)
	}

	// 4. Accept invite with second user (teammate)
	teammateID, teammateHdr := e.addUser(t, "teammate")
	acceptResp := e.post(t, fmt.Sprintf("/api/v1/invites/%s/accept", invRes.Token), "", teammateHdr)
	if acceptResp.StatusCode != fiber.StatusOK {
		t.Fatalf("accept status = %d", acceptResp.StatusCode)
	}
	var acceptRes AcceptInviteResponse
	_ = json.NewDecoder(acceptResp.Body).Decode(&acceptRes)
	if acceptRes.OrgID != orgID || acceptRes.Role != "member" {
		t.Fatalf("unexpected accept response: %+v", acceptRes)
	}

	// Verify teammate is now member in DB
	resp = e.do(t, "GET", fmt.Sprintf("/api/v1/orgs/%d/members", orgID), nil, teammateHdr)
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("expected teammate to access members, got %d", resp.StatusCode)
	}
	_ = json.NewDecoder(resp.Body).Decode(&listRes)
	if len(listRes.Members) != 2 {
		t.Fatalf("expected 2 members, got %d", len(listRes.Members))
	}

	// 5. Re-accepting should fail with 409 Conflict
	secondAccept := e.post(t, fmt.Sprintf("/api/v1/invites/%s/accept", invRes.Token), "", teammateHdr)
	if secondAccept.StatusCode != fiber.StatusConflict {
		t.Fatalf("expected 409 conflict on re-accept, got %d", secondAccept.StatusCode)
	}

	// 6. Owner updates teammate's role to 'admin'
	updatePayload := `{"role":"admin"}`
	patchReq := httptest.NewRequest("PATCH", fmt.Sprintf("/api/v1/orgs/%d/members/%d", orgID, teammateID), strings.NewReader(updatePayload))
	patchReq.Header.Set("Authorization", ownerHdr["Authorization"])
	patchReq.Header.Set("Content-Type", "application/json")
	patchResp, _ := e.app.Test(patchReq)
	if patchResp.StatusCode != fiber.StatusOK {
		t.Fatalf("patch role status = %d", patchResp.StatusCode)
	}

	resp = e.do(t, "GET", fmt.Sprintf("/api/v1/orgs/%d/members", orgID), nil, ownerHdr)
	_ = json.NewDecoder(resp.Body).Decode(&listRes)
	var foundUpdated bool
	for _, m := range listRes.Members {
		if m.UserID == teammateID && m.Role == "admin" {
			foundUpdated = true
		}
	}
	if !foundUpdated {
		t.Fatalf("expected teammate role to be updated to admin: %+v", listRes.Members)
	}

	// 7. Remove teammate
	delReq := httptest.NewRequest("DELETE", fmt.Sprintf("/api/v1/orgs/%d/members/%d", orgID, teammateID), nil)
	delReq.Header.Set("Authorization", ownerHdr["Authorization"])
	delResp, _ := e.app.Test(delReq)
	if delResp.StatusCode != fiber.StatusNoContent {
		t.Fatalf("delete member status = %d", delResp.StatusCode)
	}

	resp = e.do(t, "GET", fmt.Sprintf("/api/v1/orgs/%d/members", orgID), nil, teammateHdr)
	if resp.StatusCode != fiber.StatusNotFound {
		t.Fatalf("expected 404 for removed member, got %d", resp.StatusCode)
	}
}
