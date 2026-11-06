-- Release artifacts for mobile & web crash symbolication:
-- dSYM bundles (iOS Mach-O/DWARF), ProGuard / R8 mappings (Android), and JS source maps.

CREATE TABLE IF NOT EXISTS project_release_artifacts (
    id               BIGSERIAL PRIMARY KEY,
    project_id       BIGINT       NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    version          VARCHAR(64)  NOT NULL,
    build_number     VARCHAR(64)  NOT NULL DEFAULT '',
    artifact_type    VARCHAR(32)  NOT NULL, -- 'dsym', 'proguard_mapping', 'sourcemap'
    file_name        VARCHAR(255) NOT NULL,
    storage_path     VARCHAR(512) NOT NULL DEFAULT '',
    content          BYTEA,                 -- stored directly for fast access or fallback
    checksum_sha256  VARCHAR(64)  NOT NULL,
    cpu_arch         VARCHAR(32)  NOT NULL DEFAULT '', -- 'arm64', 'arm64e', 'x86_64'
    debug_uuid       VARCHAR(64)  NOT NULL DEFAULT '', -- Mach-O UUID or ProGuard identifier
    size_bytes       BIGINT       NOT NULL DEFAULT 0,
    created_at       TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_release_artifacts_lookup
    ON project_release_artifacts(project_id, version, artifact_type);

CREATE INDEX IF NOT EXISTS idx_release_artifacts_uuid
    ON project_release_artifacts(project_id, debug_uuid)
    WHERE debug_uuid <> '';
