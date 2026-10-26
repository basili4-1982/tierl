-- Server-side sessions (ADR-003). The cookie contains a random 32-byte token;
-- only its SHA-256 is stored here. Deleting a user removes their sessions.
CREATE TABLE sessions (
    token_hash text        PRIMARY KEY,
    user_id    text        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    CONSTRAINT sessions_token_hash_check CHECK (char_length(token_hash) = 64),
    CONSTRAINT sessions_user_id_check CHECK (btrim(user_id) <> ''),
    CONSTRAINT sessions_times_check CHECK (expires_at > created_at)
);

CREATE INDEX sessions_user_id_idx ON sessions (user_id);
CREATE INDEX sessions_expires_at_idx ON sessions (expires_at);
