-- A placement puts one item into one tier within a submission. Composite
-- foreign keys guarantee the item, the tier and the submission belong to the
-- same tier list. Skipped items have no row.
CREATE TABLE placements (
    submission_id text NOT NULL,
    tierlist_id   text NOT NULL,
    item_id       text NOT NULL,
    tier_id       text NOT NULL,
    PRIMARY KEY (submission_id, item_id),
    FOREIGN KEY (tierlist_id, submission_id) REFERENCES submissions (tierlist_id, id) ON DELETE CASCADE,
    FOREIGN KEY (tierlist_id, item_id) REFERENCES items (tierlist_id, id) ON DELETE CASCADE,
    FOREIGN KEY (tierlist_id, tier_id) REFERENCES tiers (tierlist_id, id) ON DELETE CASCADE
);

-- Statistics scans and FK support for deletes of items and tiers.
CREATE INDEX placements_item_idx ON placements (tierlist_id, item_id, tier_id);
CREATE INDEX placements_tier_idx ON placements (tierlist_id, tier_id);
