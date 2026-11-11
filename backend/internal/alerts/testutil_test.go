package alerts

import (
	"context"
	"os"
	"strconv"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tiofani03/artracker/backend/internal/db"
)

// testDB connects to TEST_DATABASE_URL, migrates, and truncates the tables so
// every test starts empty. Skips when the variable is unset. Packages share one
// database, so run with -p 1.
func testDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := db.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if _, err := pool.Exec(ctx, `TRUNCATE users, organizations, organization_members, projects, api_keys, refresh_tokens RESTART IDENTITY CASCADE`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	return pool
}

// seedProject inserts an org and a project with the default alert rules.
func seedProject(t *testing.T, pool *pgxpool.Pool) (orgID, projectID int64) {
	t.Helper()
	ctx := context.Background()
	if err := pool.QueryRow(ctx, `INSERT INTO organizations(name, slug) VALUES ('Acme','acme') RETURNING id`).Scan(&orgID); err != nil {
		t.Fatalf("seed org: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO projects(org_id, name, slug, platform) VALUES ($1,'Checkout Web','checkout-web','web') RETURNING id`, orgID).Scan(&projectID); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	if _, err := pool.Exec(ctx, SeedRulesSQL, projectID); err != nil {
		t.Fatalf("seed rules: %v", err)
	}
	return orgID, projectID
}

var issueSeq int

// seedIssue inserts an issue with the given status and returns its id.
func seedIssue(t *testing.T, pool *pgxpool.Pool, projectID int64, status string) int64 {
	t.Helper()
	issueSeq++
	var id int64
	err := pool.QueryRow(context.Background(),
		`INSERT INTO issues(project_id, fingerprint, title, status) VALUES ($1, $2, 'TypeError: boom', $3) RETURNING id`,
		projectID, "fp-"+strconv.Itoa(issueSeq), status).Scan(&id)
	if err != nil {
		t.Fatalf("seed issue: %v", err)
	}
	return id
}

type sentMsg struct {
	ChatID int64
	HTML   string
}

// fakeNotifier records messages; errFor returns a canned error per chat id.
type fakeNotifier struct {
	mu     sync.Mutex
	sent   []sentMsg
	errFor map[int64]error
}

func (f *fakeNotifier) SendMessage(_ context.Context, chatID int64, html string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.errFor[chatID]; err != nil {
		return err
	}
	f.sent = append(f.sent, sentMsg{ChatID: chatID, HTML: html})
	return nil
}
