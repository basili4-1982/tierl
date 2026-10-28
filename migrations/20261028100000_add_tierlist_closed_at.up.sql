-- Keep the closing moment separately from updated_at so editing the title or
-- description of a closed tier list does not move closed_at.
ALTER TABLE tierlists ADD COLUMN closed_at timestamptz;

-- Backfill already closed tier lists: before this change closing was the last
-- update, so updated_at is the best available approximation.
UPDATE tierlists SET closed_at = updated_at WHERE status = 'closed';
