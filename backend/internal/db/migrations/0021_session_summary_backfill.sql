-- Backfill the session list summary (0020) from the last 30 days of telemetry (replay chunk uploads excluded) and
-- crash occurrences; the list window is 7 days, so older sessions are never listed by default. Same horizon and
-- partition pruning as the 0016 backfill. Idempotent: re-running recomputes the summary from the raw rows.
INSERT INTO session_contexts (project_id, session_id, user_id, first_seen, last_seen, has_error,
                              environment, event_count, has_rage_click, has_dead_click)
SELECT src.project_id,
       src.session_id,
       COALESCE((array_agg(src.user_id ORDER BY src.ts) FILTER (WHERE src.user_id <> ''))[1], ''),
       min(src.ts),
       max(src.ts),
       bool_or(src.crash OR src.level IN ('error', 'fatal')),
       COALESCE((array_agg(src.environment ORDER BY src.ts) FILTER (WHERE src.environment <> ''))[1], ''),
       count(*),
       bool_or(src.name IN ('rage_click', 'rage_tap')),
       bool_or(src.name = 'dead_click')
FROM (
    SELECT project_id, session_id, user_id, environment, event_timestamp AS ts, level, name, false AS crash
    FROM telemetry_events
    WHERE session_id <> '' AND name <> 'replay_chunk'
      AND event_timestamp > now() - interval '30 days'
      AND received_at > now() - interval '31 days' -- prunes partitions (the table is partitioned by received_at)
    UNION ALL
    SELECT i.project_id, ie.session_id, ie.user_id, COALESCE(ie.environment, ''), ie.created_at, 'fatal', '', true
    FROM issue_events ie
    JOIN issues i ON i.id = ie.issue_id
    WHERE ie.session_id <> '' AND ie.created_at > now() - interval '30 days'
) src
JOIN projects p ON p.id = src.project_id
GROUP BY src.project_id, src.session_id
ON CONFLICT (project_id, session_id) DO UPDATE SET
    user_id        = CASE WHEN session_contexts.user_id = '' THEN EXCLUDED.user_id ELSE session_contexts.user_id END,
    first_seen     = LEAST(session_contexts.first_seen, EXCLUDED.first_seen),
    last_seen      = GREATEST(session_contexts.last_seen, EXCLUDED.last_seen),
    has_error      = session_contexts.has_error OR EXCLUDED.has_error,
    environment    = EXCLUDED.environment,
    event_count    = EXCLUDED.event_count,
    has_rage_click = EXCLUDED.has_rage_click,
    has_dead_click = EXCLUDED.has_dead_click,
    updated_at     = now();
