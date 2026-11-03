-- Starter schema. Replace/extend once the real domain is settled.
CREATE TABLE IF NOT EXISTS artworks (
    id         BIGSERIAL PRIMARY KEY,
    title      TEXT        NOT NULL,
    artist     TEXT        NOT NULL DEFAULT '',
    year       INTEGER,
    medium     TEXT        NOT NULL DEFAULT '',
    notes      TEXT        NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS artworks_created_at_idx ON artworks (created_at DESC);
CREATE INDEX IF NOT EXISTS artworks_title_idx ON artworks (lower(title));
