package db

import (
	"context"
	"os"
	"testing"
)

func TestMigrateCreatesAuthTables(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	for _, table := range []string{"users", "organizations", "organization_members", "projects", "api_keys", "refresh_tokens", "telemetry_events"} {
		var ok bool
		if err := pool.QueryRow(ctx, `SELECT to_regclass('public.`+table+`') IS NOT NULL`).Scan(&ok); err != nil || !ok {
			t.Errorf("table %s missing (err=%v)", table, err)
		}
	}

	if _, err := pool.Exec(ctx, `TRUNCATE users, organizations, organization_members, projects, api_keys, refresh_tokens RESTART IDENTITY CASCADE`); err != nil {
		t.Skipf("tables not ready: %v", err)
	}

	// invalid role is rejected by the CHECK constraint
	var userID, orgID int64
	if err := pool.QueryRow(ctx, `INSERT INTO users(email, sso_provider, sso_id) VALUES ('a@x.io','google','1') RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO organizations(name, slug) VALUES ('Acme','acme') RETURNING id`).Scan(&orgID); err != nil {
		t.Fatalf("insert org: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO organization_members(org_id, user_id, role) VALUES ($1,$2,'god')`, orgID, userID); err == nil {
		t.Error("expected CHECK violation for role 'god'")
	}

	// Google is the only supported SSO provider
	if _, err := pool.Exec(ctx, `INSERT INTO users(email, sso_provider, sso_id) VALUES ('c@x.io','github','2')`); err == nil {
		t.Error("expected CHECK violation for sso_provider 'github'")
	}

	// (sso_provider, sso_id) must be unique
	if _, err := pool.Exec(ctx, `INSERT INTO users(email, sso_provider, sso_id) VALUES ('b@x.io','google','1')`); err == nil {
		t.Error("expected unique violation for duplicate (sso_provider, sso_id)")
	}
}
