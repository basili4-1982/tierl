-- One-time tokens for email confirmation and password reset (ADR-003).
-- The raw token travels only in the email; the database stores its SHA-256,
-- so a database leak cannot be replayed. A token is single-use (used_at) and
-- expires (expires_at). Issuing a new token for the same purpose marks the
-- previous one used.
CREATE TABLE one_time_tokens (
    id          text        PRIMARY KEY,
    user_id     text        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    purpose     text        NOT NULL,
    token_hash  bytea       NOT NULL,
    expires_at  timestamptz NOT NULL,
    used_at     timestamptz,
    created_at  timestamptz NOT NULL,
    CONSTRAINT one_time_tokens_hash_key UNIQUE (token_hash),
    CONSTRAINT one_time_tokens_id_check CHECK (btrim(id) <> ''),
    CONSTRAINT one_time_tokens_purpose_check CHECK (purpose IN ('verify_email', 'reset_password')),
    CONSTRAINT one_time_tokens_hash_check CHECK (octet_length(token_hash) = 32),
    CONSTRAINT one_time_tokens_times_check CHECK (
        expires_at > created_at
        AND (used_at IS NULL OR used_at >= created_at)
    )
);

-- At most one active token per user and purpose: a concurrent reissue cannot
-- leave two working links. The index also serves lookups by user and purpose.
CREATE UNIQUE INDEX one_time_tokens_active_key ON one_time_tokens (user_id, purpose) WHERE used_at IS NULL;
-- Removing dead rows.
CREATE INDEX one_time_tokens_expires_at_idx ON one_time_tokens (expires_at);
