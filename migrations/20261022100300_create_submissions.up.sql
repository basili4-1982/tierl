-- One anonymous answer per session and tier list (ADR-002). session_id holds
-- the hash of the session token, never the token itself. There is no link to
-- a user account by design.
CREATE TABLE submissions (
    id          text        PRIMARY KEY,
    tierlist_id text        NOT NULL REFERENCES tierlists (id) ON DELETE CASCADE,
    session_id  text        NOT NULL,
    created_at  timestamptz NOT NULL,
    updated_at  timestamptz NOT NULL,
    CONSTRAINT submissions_session_key UNIQUE (tierlist_id, session_id),
    -- Target of composite foreign keys that keep placements inside one tier list.
    CONSTRAINT submissions_tierlist_id_key UNIQUE (tierlist_id, id),
    CONSTRAINT submissions_id_check CHECK (btrim(id) <> ''),
    CONSTRAINT submissions_session_check CHECK (btrim(session_id) <> ''),
    CONSTRAINT submissions_times_check CHECK (updated_at >= created_at)
);

-- Newest answers first, used for similarity scans and counts per tier list.
CREATE INDEX submissions_tierlist_idx ON submissions (tierlist_id, created_at DESC);
