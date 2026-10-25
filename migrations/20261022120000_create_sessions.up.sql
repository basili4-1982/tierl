-- Server-side sessions (ADR-003). Only the SHA-256 of the cookie token is
-- stored; the cookie carries 32 random bytes. expires_at slides on activity
-- but never past absolute_expires_at. Revoked sessions keep their row so that
-- "logout all devices" and password resets stay auditable.
CREATE TABLE sessions (
    token_hash          bytea       PRIMARY KEY,
    user_id             text        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    csrf_secret         bytea       NOT NULL,
    created_at          timestamptz NOT NULL,
    last_seen_at        timestamptz NOT NULL,
    expires_at          timestamptz NOT NULL,
    absolute_expires_at timestamptz NOT NULL,
    revoked_at          timestamptz,
    CONSTRAINT sessions_token_hash_check CHECK (octet_length(token_hash) = 32),
    CONSTRAINT sessions_csrf_secret_check CHECK (octet_length(csrf_secret) = 32),
    CONSTRAINT sessions_times_check CHECK (
        last_seen_at >= created_at
        AND expires_at >= created_at
        AND absolute_expires_at >= expires_at
    )
);

CREATE INDEX sessions_user_id_idx ON sessions (user_id);
CREATE INDEX sessions_expires_at_idx ON sessions (expires_at);
