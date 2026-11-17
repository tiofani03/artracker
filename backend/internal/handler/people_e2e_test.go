package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/tiofani03/artracker/backend/internal/ingest"
	"github.com/tiofani03/artracker/backend/internal/people"
)

// TestIngestToPeopleEndToEnd sends the envelopes the Android and iOS SDKs produce through POST /api/v1/ingest,
// lets the DB-backed worker flush them through the people row processor, and reads them back via the People API.
func TestIngestToPeopleEndToEnd(t *testing.T) {
	e := newTestEnv(t)
	svc := ingest.NewService(ingest.Config{Workers: 1, QueueSize: 16, BatchSize: 100, FlushIntervalMs: 20,
		ProjectRPS: 100, ProjectEventsPS: 1000, KeyCacheTTLSec: 60}, e.pool)
	svc.AddRowProcessor(people.NewProcessor(people.NewStore(e.pool)))
	e.h.WithIngest(svc)
	t.Cleanup(func() { _ = svc.Shutdown(context.Background()) })

	uid, hdr := e.addUser(t, "people-e2e")
	org := e.addOrg(t, "people-e2e-org")
	e.addMember(t, org, uid, "owner")
	pid := e.addProject(t, org, "people-e2e-app")
	key := e.createKey(t, pid, "public_client", hdr)
	t.Cleanup(func() {
		_, _ = e.pool.Exec(context.Background(), `DELETE FROM telemetry_events WHERE project_id = $1`, pid)
	})
	keyHdr := map[string]string{"X-Artracker-Key": key.Secret, "Content-Type": "application/json"}

	now := time.Now().UTC().Truncate(time.Millisecond)
	ts := func(d time.Duration) string { return now.Add(d).Format(time.RFC3339Nano) }

	android := fmt.Sprintf(`{"sent_at":%q,"sdk":{"name":"artracker-android","version":"1.0.3"},
	  "context":{"environment":"production","app_version":"2.3.1","sdk_platform":"android"},
	  "items":[
	    {"type":"session","name":"session_start","timestamp":%q,"session_id":"ses_and_1","user_id":"u_android",
	     "attributes":{"platform":"android","model":"Pixel 8","manufacturer":"Google","os_version":"14","app_version":"2.3.1"}},
	    {"type":"event","name":"$identify","timestamp":%q,"session_id":"ses_and_1","user_id":"u_android",
	     "attributes":{"plan":"pro","country":"ID","password":"hunter2"}},
	    {"type":"log","name":"exception","level":"error","message":"boom","timestamp":%q,"session_id":"ses_and_1","user_id":"u_android"}
	  ]}`, ts(0), ts(0), ts(time.Second), ts(2*time.Second))
	ios := fmt.Sprintf(`{"sent_at":%q,"sdk":{"name":"artracker-ios","version":"0.2.0"},
	  "context":{"environment":"production","app_version":"5.0 (50)","os_name":"iOS"},
	  "items":[
	    {"type":"session","name":"session.start","timestamp":%q,"session_id":"ses_ios_1",
	     "attributes":{"platform":"ios","model":"iPhone15,2","os_version":"17.4.1","launch_type":"cold"}},
	    {"type":"event","name":"$identify","timestamp":%q,"session_id":"ses_ios_1","user_id":"u_ios","attributes":{"plan":"free"}}
	  ]}`, ts(0), ts(0), ts(time.Second))

	for name, body := range map[string]string{"android": android, "ios": ios} {
		if resp := e.post(t, "/api/v1/ingest", body, keyHdr); resp.StatusCode != 202 && resp.StatusCode != 200 {
			t.Fatalf("%s ingest status %d", name, resp.StatusCode)
		}
	}

	base := fmt.Sprintf("/api/v1/projects/%d", pid)
	var list PeopleListResponse
	deadline := time.Now().Add(5 * time.Second)
	for {
		resp := e.do(t, "GET", base+"/people", nil, hdr)
		decodeJSON(t, resp, &list)
		if list.Total == 2 || time.Now().After(deadline) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if list.Total != 2 {
		t.Fatalf("want 2 people after flush, got %+v", list)
	}

	var and PersonDetailResponse
	decodeJSON(t, e.do(t, "GET", base+"/people/u_android", nil, hdr), &and)
	if and.Person.Traits["plan"] != "pro" || and.Person.Traits["password"] != nil || and.Person.ErrorSessionCount != 1 {
		t.Fatalf("android person: %+v", and.Person)
	}
	if and.Person.LastContext["model"] != "Pixel 8" || len(and.Sessions) != 1 || !and.Sessions[0].HasError {
		t.Fatalf("android context/sessions: %+v %+v", and.Person.LastContext, and.Sessions)
	}

	// iOS: the session started anonymous, then identified; the session belongs to the user and keeps its context.
	var ctx SessionContextResponse
	decodeJSON(t, e.do(t, "GET", base+"/sessions/ses_ios_1/context", nil, hdr), &ctx)
	if ctx.UserID != "u_ios" || ctx.Context["model"] != "iPhone15,2" || ctx.Person == nil || ctx.Person.Traits["plan"] != "free" {
		t.Fatalf("ios context: %+v", ctx)
	}
}

func decodeJSON(t *testing.T, resp *http.Response, out any) {
	t.Helper()
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		t.Fatal(err)
	}
}
