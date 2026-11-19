package appstart

import (
	"context"

	"github.com/jackc/pgx/v5/pgconn"
)

// Execer is the slice of a pgx pool (or transaction) Requeue needs.
type Execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// Requeue makes the worker retry, immediately and from a clean attempt count, the project's profiles that
// are waiting on symbols (status missing_symbols, including ones the backoff gave up on) and that a new
// artifact can resolve: profiles of release version, and profiles containing an image whose dSYM debugUUID
// was just uploaded. Pass "" for the key that does not apply. debugUUID is matched case-insensitively
// against image_uuids, which holds dashed upper-case UUIDs.
func Requeue(ctx context.Context, db Execer, projectID int64, version, debugUUID string) error {
	if version == "" && debugUUID == "" {
		return nil
	}
	_, err := db.Exec(ctx, `
		UPDATE app_start_profiles
		SET status = 'pending', next_attempt_at = now(), attempts = 0
		WHERE project_id = $1 AND status = 'missing_symbols'
		  AND (($2::text <> '' AND app_version = $2::text)
		    OR ($3::text <> '' AND upper($3::text) = ANY (image_uuids)))`,
		projectID, version, debugUUID)
	return err
}
