DROP VIEW IF EXISTS moderation_log;
DROP TABLE IF EXISTS moderation_audit;
DROP VIEW IF EXISTS tierlist_reports;
DROP TABLE IF EXISTS reports;

ALTER TABLE tierlists
    DROP CONSTRAINT IF EXISTS tierlists_reports_count_check,
    DROP COLUMN IF EXISTS reports_count,
    DROP COLUMN IF EXISTS hidden_reason,
    DROP COLUMN IF EXISTS hidden_at;
