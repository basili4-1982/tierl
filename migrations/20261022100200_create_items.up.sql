CREATE TABLE items (
    tierlist_id text    NOT NULL REFERENCES tierlists (id) ON DELETE CASCADE,
    id          text    NOT NULL,
    label       text    NOT NULL,
    image_url   text,
    position    integer NOT NULL,
    PRIMARY KEY (tierlist_id, id),
    CONSTRAINT items_position_key UNIQUE (tierlist_id, position) DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT items_id_check CHECK (btrim(id) <> ''),
    CONSTRAINT items_label_check CHECK (btrim(label) <> '' AND char_length(label) <= 80),
    CONSTRAINT items_image_url_check CHECK (image_url IS NULL OR (image_url <> '' AND char_length(image_url) <= 2048)),
    CONSTRAINT items_position_check CHECK (position >= 0 AND position < 50)
);
