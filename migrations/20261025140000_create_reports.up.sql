-- Reports and moderation for public tier lists. Existing migrations are not
-- modified: the tierlists table gains the moderation bookkeeping below.
ALTER TABLE tierlists
    ADD COLUMN hidden_at     timestamptz,
    ADD COLUMN hidden_reason text,
    ADD COLUMN reports_count integer NOT NULL DEFAULT 0,
    ADD CONSTRAINT tierlists_reports_count_check CHECK (reports_count >= 0);

-- One report per reporter for a tier list. The reporter is a stable hash of
-- the account/session identity; raw IP addresses and tokens are never stored.
CREATE TABLE reports (
    id               text        PRIMARY KEY DEFAULT gen_random_uuid()::text,
    tierlist_id      text        NOT NULL REFERENCES tierlists(id) ON DELETE CASCADE,
    reporter_ip_hash text        NOT NULL,
    reporter_key     text        GENERATED ALWAYS AS (reporter_ip_hash) STORED,
    reason           text        NOT NULL DEFAULT 'other',
    comment          text        NOT NULL DEFAULT '',
    created_at       timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT reports_id_check CHECK (btrim(id) <> ''),
    CONSTRAINT reports_reporter_check CHECK (btrim(reporter_ip_hash) <> ''),
    CONSTRAINT reports_reason_check CHECK (btrim(reason) <> '' AND char_length(reason) <= 100),
    CONSTRAINT reports_comment_check CHECK (char_length(comment) <= 2000),
    CONSTRAINT reports_unique_reporter UNIQUE (tierlist_id, reporter_ip_hash)
);

CREATE INDEX reports_tierlist_idx ON reports (tierlist_id, created_at DESC);

-- Compatibility alias used by the moderation notes: the same rows and
-- columns under the longer table name.
CREATE VIEW tierlist_reports AS
SELECT id, tierlist_id, reporter_ip_hash, reporter_key, reason, comment, created_at
FROM reports;

-- Every moderation decision is append-only. Auto-hide writes actor_role
-- 'system'; manual decisions record the acting account when one is known.
CREATE TABLE moderation_audit (
    id            bigserial   PRIMARY KEY,
    tierlist_id   text        NOT NULL REFERENCES tierlists(id) ON DELETE CASCADE,
    actor_id      text,
    actor_role    text        NOT NULL DEFAULT 'system',
    action        text        NOT NULL,
    reason        text        NOT NULL DEFAULT '',
    reports_count integer     NOT NULL DEFAULT 0,
    created_at    timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT moderation_audit_action_check CHECK (btrim(action) <> ''),
    CONSTRAINT moderation_audit_role_check CHECK (btrim(actor_role) <> ''),
    CONSTRAINT moderation_audit_reports_check CHECK (reports_count >= 0)
);

CREATE INDEX moderation_audit_tierlist_idx ON moderation_audit (tierlist_id, created_at DESC);

CREATE VIEW moderation_log AS
SELECT id, tierlist_id, actor_id, actor_role, action, reason, reports_count, created_at
FROM moderation_audit;
