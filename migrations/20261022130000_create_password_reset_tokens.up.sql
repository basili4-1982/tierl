-- One-time password reset tokens (ADR-003). The token itself is 32 random
-- bytes sent by email; the database stores only its SHA-256. used_at makes a
-- token single-use, expires_at bounds its lifetime (30 minutes by default).
CREATE TABLE password_reset_tokens (
    token_hash bytea       PRIMARY KEY,
    user_id    text        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    used_at    timestamptz,
    CONSTRAINT password_reset_tokens_token_hash_check CHECK (octet_length(token_hash) = 32),
    CONSTRAINT password_reset_tokens_times_check CHECK (expires_at > created_at),
    CONSTRAINT password_reset_tokens_used_check CHECK (used_at IS NULL OR used_at >= created_at)
);

CREATE INDEX password_reset_tokens_user_id_idx ON password_reset_tokens (user_id);
