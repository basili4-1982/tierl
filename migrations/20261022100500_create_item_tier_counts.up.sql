-- Pre-aggregate "item x tier": how many submissions put the item in the tier.
-- Maintained by the repository in the same transaction as the placements.
CREATE TABLE item_tier_counts (
    tierlist_id text    NOT NULL,
    item_id     text    NOT NULL,
    tier_id     text    NOT NULL,
    count       integer NOT NULL DEFAULT 0,
    PRIMARY KEY (tierlist_id, item_id, tier_id),
    FOREIGN KEY (tierlist_id, item_id) REFERENCES items (tierlist_id, id) ON DELETE CASCADE,
    FOREIGN KEY (tierlist_id, tier_id) REFERENCES tiers (tierlist_id, id) ON DELETE CASCADE,
    CONSTRAINT item_tier_counts_count_check CHECK (count >= 0)
);

CREATE INDEX item_tier_counts_tier_idx ON item_tier_counts (tierlist_id, tier_id);
