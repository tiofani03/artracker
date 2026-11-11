package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestCreateProjectSeedsAlertRules(t *testing.T) {
	e := newTestEnv(t)
	org := e.addOrg(t, "acme-alerts")
	uid, hdr := e.addUser(t, "alertadmin")
	e.addMember(t, org, uid, "admin")

	resp := e.post(t, fmt.Sprintf("/api/v1/orgs/%d/projects", org), `{"name":"Alerts App","platform":"web"}`, hdr)
	if resp.StatusCode != 201 {
		t.Fatalf("create project = %d", resp.StatusCode)
	}
	var p struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
		t.Fatal(err)
	}

	q := fmt.Sprintf(`SELECT count(*) FROM alert_rules WHERE project_id=%d AND enabled AND environment='production'`, p.ID)
	if n := e.count(t, q); n != 3 {
		t.Fatalf("seeded enabled production rules = %d, want 3", n)
	}
}

func TestMigrationColumns(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()
	if _, err := e.pool.Exec(ctx, `SELECT environment FROM issue_events LIMIT 0`); err != nil {
		t.Fatalf("issue_events.environment: %v", err)
	}

	org := e.addOrg(t, "acme-mig")
	pid := e.addProject(t, org, "mig-app")
	e.addIssue(t, pid, "fp-mig", "open")
	var issueID int64
	if err := e.pool.QueryRow(ctx, `SELECT id FROM issues WHERE project_id=$1`, pid).Scan(&issueID); err != nil {
		t.Fatal(err)
	}

	const ins = `INSERT INTO alert_incidents(project_id, rule_kind, issue_id, status, title) VALUES ($1,'new_issue',$2,$3,'t')`
	if _, err := e.pool.Exec(ctx, ins, pid, issueID, "firing"); err != nil {
		t.Fatal(err)
	}
	_, err := e.pool.Exec(ctx, ins, pid, issueID, "firing")
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		t.Fatalf("second firing insert err = %v, want unique violation 23505", err)
	}

	if _, err := e.pool.Exec(ctx, `UPDATE alert_incidents SET status='resolved' WHERE project_id=$1`, pid); err != nil {
		t.Fatal(err)
	}
	if _, err := e.pool.Exec(ctx, ins, pid, issueID, "firing"); err != nil {
		t.Fatalf("firing after resolved should succeed: %v", err)
	}
}

func TestInsertProjectRollsBackWhenSeedFails(t *testing.T) {
	e := newTestEnv(t)
	org := e.addOrg(t, "acme-atomic")
	boom := errors.New("seed boom")

	_, err := e.h.insertProject(context.Background(), org, "Atomic", "atomic-app", "web",
		func(context.Context, pgx.Tx, int64) error { return boom })
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want wrapped seed error", err)
	}
	if n := e.count(t, `SELECT count(*) FROM projects WHERE slug='atomic-app'`); n != 0 {
		t.Fatalf("project rows after failed seed = %d, want 0", n)
	}

	// The slug stays free, so a retry succeeds and gets its rules.
	p, err := e.h.insertProject(context.Background(), org, "Atomic", "atomic-app", "web", seedAlertRules)
	if err != nil {
		t.Fatal(err)
	}
	if n := e.count(t, fmt.Sprintf(`SELECT count(*) FROM alert_rules WHERE project_id=%d`, p.ID)); n != 3 {
		t.Fatalf("rules = %d, want 3", n)
	}
}
