-- Users for email + password auth (ADR-003). Email is stored lower-case and is
-- unique; password_hash is a PHC string (argon2id) that carries its own parameters.
-- tierlists.author_id has no foreign key yet; adding it needs a backfill decision
-- and is left to a separate migration.
CREATE TABLE users (
    id             text        PRIMARY KEY,
    email          text        NOT NULL,
    display_name   text        NOT NULL,
    password_hash  text        NOT NULL,
    email_verified boolean     NOT NULL DEFAULT false,
    created_at     timestamptz NOT NULL,
    updated_at     timestamptz NOT NULL,
    CONSTRAINT users_email_key UNIQUE (email),
    CONSTRAINT users_id_check CHECK (btrim(id) <> ''),
    CONSTRAINT users_email_check CHECK (email = lower(email) AND char_length(email) BETWEEN 3 AND 254),
    CONSTRAINT users_display_name_check CHECK (btrim(display_name) <> '' AND char_length(display_name) <= 50),
    CONSTRAINT users_password_hash_check CHECK (btrim(password_hash) <> ''),
    CONSTRAINT users_times_check CHECK (updated_at >= created_at)
);
