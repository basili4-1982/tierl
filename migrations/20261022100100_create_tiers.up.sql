CREATE TABLE tiers (
    tierlist_id text    NOT NULL REFERENCES tierlists (id) ON DELETE CASCADE,
    id          text    NOT NULL,
    name        text    NOT NULL,
    color       text    NOT NULL,
    position    integer NOT NULL,
    PRIMARY KEY (tierlist_id, id),
    -- Deferrable so tiers can be reordered inside one transaction.
    CONSTRAINT tiers_position_key UNIQUE (tierlist_id, position) DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT tiers_id_check CHECK (btrim(id) <> ''),
    CONSTRAINT tiers_name_check CHECK (btrim(name) <> '' AND char_length(name) <= 40),
    CONSTRAINT tiers_color_check CHECK (color ~ '^#[0-9a-fA-F]{6}$'),
    CONSTRAINT tiers_position_check CHECK (position >= 0 AND position < 10)
);
