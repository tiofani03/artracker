package handler

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

type chunkListFixture struct {
	e       *testEnv
	pid     int64
	session string
	hdr     map[string]string
}

// newChunkListFixture creates a project whose session has chunks 0, 1 and 2 acknowledged.
func newChunkListFixture(t *testing.T) chunkListFixture {
	t.Helper()
	e := newTestEnv(t)
	e.h.WithReplayStore(&mockReplayStore{})
	uid, authHdr := e.addUser(t, "chunklister")
	org := e.addOrg(t, "acme-chunk-list")
	e.addMember(t, org, uid, "owner")
	pid := e.addProject(t, org, "chunk-list-app")
	key := e.createKey(t, pid, "public_client", authHdr)
	keyHdr := map[string]string{"X-Artracker-Key": key.Secret, "Content-Type": "application/json"}

	session := "sess_chunks_12345678"
	for seq := 0; seq < 3; seq++ {
		body := fmt.Sprintf(`{"session_id":%q,"chunk_seq":%d,"size_bytes":1024}`, session, seq)
		if got := e.post(t, "/api/v1/replay/chunk-url", body, keyHdr).StatusCode; got != 200 {
			t.Fatalf("chunk-url %d = %d", seq, got)
		}
		ack := fmt.Sprintf(`{"session_id":%q,"chunk_seq":%d,"size_bytes":1024,"duration_ms":%d,"event_count":10}`, session, seq, (seq+1)*10000)
		if got := e.post(t, "/api/v1/replay/chunk-ack", ack, keyHdr).StatusCode; got != 200 {
			t.Fatalf("chunk-ack %d = %d", seq, got)
		}
	}
	return chunkListFixture{e: e, pid: pid, session: session, hdr: authHdr}
}

func (f chunkListFixture) list(t *testing.T, sessionID, query string) (int, []ReplayChunkDetail) {
	t.Helper()
	path := fmt.Sprintf("/api/v1/projects/%d/sessions/%s/chunks%s", f.pid, sessionID, query)
	resp := f.e.do(t, "GET", path, strings.NewReader(""), f.hdr)
	var out ReplayChunkListResponse
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out.Chunks
}

func TestListSessionChunksRange(t *testing.T) {
	f := newChunkListFixture(t)
	status, chunks := f.list(t, f.session, "?from_seq=1&to_seq=2")
	if status != 200 {
		t.Fatalf("status = %d", status)
	}
	if len(chunks) != 2 || chunks[0].ChunkSeq != 1 || chunks[1].ChunkSeq != 2 {
		t.Fatalf("chunks = %+v, want seq 1 and 2", chunks)
	}
	for _, c := range chunks {
		if c.URL == "" {
			t.Fatalf("chunk %d has no signed url", c.ChunkSeq)
		}
	}
}

func TestListSessionChunksDefaultsAndLimit(t *testing.T) {
	f := newChunkListFixture(t)
	status, chunks := f.list(t, f.session, "")
	if status != 200 || len(chunks) != 3 {
		t.Fatalf("status = %d, chunks = %d, want 200 and 3", status, len(chunks))
	}
	for i, c := range chunks {
		if c.ChunkSeq != i {
			t.Fatalf("chunks must be ascending, got %+v", chunks)
		}
	}
}

func TestListSessionChunksValidation(t *testing.T) {
	f := newChunkListFixture(t)
	for _, q := range []string{"?from_seq=-1", "?from_seq=abc", "?to_seq=x", "?from_seq=5&to_seq=2"} {
		if status, _ := f.list(t, f.session, q); status != 400 {
			t.Errorf("query %q status = %d, want 400", q, status)
		}
	}
	if status, chunks := f.list(t, "sess_unknown_12345678", ""); status != 200 || len(chunks) != 0 {
		t.Errorf("unknown session: status = %d, chunks = %d, want 200 and 0", status, len(chunks))
	}
	if status, _ := f.list(t, "short", ""); status != 400 {
		t.Errorf("malformed session id status = %d, want 400", status)
	}
}

func TestListSessionChunksIsolatedPerProject(t *testing.T) {
	f := newChunkListFixture(t)
	uid2, hdr2 := f.e.addUser(t, "otherowner")
	org2 := f.e.addOrg(t, "other-org")
	f.e.addMember(t, org2, uid2, "owner")
	pid2 := f.e.addProject(t, org2, "other-app")
	path := fmt.Sprintf("/api/v1/projects/%d/sessions/%s/chunks", pid2, f.session)
	resp := f.e.do(t, "GET", path, strings.NewReader(""), hdr2)
	var out ReplayChunkListResponse
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if resp.StatusCode != 200 || len(out.Chunks) != 0 {
		t.Fatalf("other project: status = %d, chunks = %d, want 200 and 0", resp.StatusCode, len(out.Chunks))
	}
}
