package db

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestLocalAuthSchema(t *testing.T) {
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
	if _, err := pool.Exec(ctx, `TRUNCATE users, organizations, organization_members, projects, api_keys, refresh_tokens RESTART IDENTITY CASCADE`); err != nil {
		t.Fatalf("truncate: %v", err)
	}

	ins := func(provider, ssoID, email string, hash any) error {
		_, err := pool.Exec(ctx, `INSERT INTO users (email, sso_provider, sso_id, password_hash) VALUES ($1, $2, $3, $4)`, email, provider, ssoID, hash)
		return err
	}

	// A Google user needs no password.
	if err := ins("google", "g1", "ada@example.com", nil); err != nil {
		t.Fatalf("google user without password: %v", err)
	}
	// A local user must have a password hash.
	if err := ins("local", "bob@example.com", "bob@example.com", nil); err == nil {
		t.Error("local user without password_hash must be rejected")
	}
	if err := ins("local", "bob@example.com", "bob@example.com", "$2a$12$x"); err != nil {
		t.Fatalf("local user with hash: %v", err)
	}
	// Providers other than google/local are still rejected.
	if err := ins("github", "x", "x@example.com", nil); err == nil {
		t.Error("unknown provider must be rejected")
	}
	// At most one password account per email, whatever the case.
	err = ins("google", "g2", "Bob@Example.com", "$2a$12$y")
	if err == nil || !strings.Contains(err.Error(), "users_password_email_uidx") {
		t.Errorf("second password account for the same email must hit users_password_email_uidx, got %v", err)
	}
	// Password-less rows with the same email stay allowed (existing Google data).
	if err := ins("google", "g3", "ada@example.com", nil); err != nil {
		t.Errorf("password-less duplicate email must stay allowed: %v", err)
	}
}
