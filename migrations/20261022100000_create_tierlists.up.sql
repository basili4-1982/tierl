-- author_id intentionally has no foreign key: the users table is created by
-- the auth migrations (separate issue), which add the constraint.
CREATE TABLE tierlists (
    id          text PRIMARY KEY,
    slug        text        NOT NULL,
    author_id   text        NOT NULL,
    title       text        NOT NULL,
    description text        NOT NULL DEFAULT '',
    visibility  text        NOT NULL,
    status      text        NOT NULL DEFAULT 'draft',
    created_at  timestamptz NOT NULL,
    updated_at  timestamptz NOT NULL,
    CONSTRAINT tierlists_slug_key UNIQUE (slug),
    CONSTRAINT tierlists_id_check CHECK (btrim(id) <> ''),
    CONSTRAINT tierlists_author_check CHECK (btrim(author_id) <> ''),
    CONSTRAINT tierlists_slug_check CHECK (slug ~ '^[A-Za-z0-9_-]{3,64}$'),
    CONSTRAINT tierlists_title_check CHECK (btrim(title) <> '' AND char_length(title) <= 120),
    CONSTRAINT tierlists_description_check CHECK (char_length(description) <= 1000),
    CONSTRAINT tierlists_visibility_check CHECK (visibility IN ('public', 'unlisted')),
    CONSTRAINT tierlists_status_check CHECK (status IN ('draft', 'open', 'closed', 'hidden')),
    -- Unlisted links must be unguessable (SPEC section 3).
    CONSTRAINT tierlists_unlisted_slug_check CHECK (visibility <> 'unlisted' OR char_length(slug) >= 12),
    CONSTRAINT tierlists_times_check CHECK (updated_at >= created_at)
);

-- Public catalog: only public + open lists, newest first (keyset pagination).
CREATE INDEX tierlists_catalog_idx ON tierlists (created_at DESC, id DESC)
    WHERE visibility = 'public' AND status = 'open';

-- "My tier lists" for the author.
CREATE INDEX tierlists_author_idx ON tierlists (author_id, created_at DESC, id DESC);
