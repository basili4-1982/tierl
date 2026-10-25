package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/basili4-1982/tierl/internal/domain"
	"github.com/basili4-1982/tierl/internal/similarity"
	"github.com/basili4-1982/tierl/internal/tierlist"
)

// CreateAnswer implements tierlist.Answers.
func (s *Store) CreateAnswer(ctx context.Context, sub tierlist.Submission, now time.Time) (tierlist.SaveResult, error) {
	return s.save(ctx, sub, now, true)
}

// Save implements tierlist.Answers.
func (s *Store) Save(ctx context.Context, sub tierlist.Submission, now time.Time) (tierlist.SaveResult, error) {
	return s.save(ctx, sub, now, false)
}

func (s *Store) save(ctx context.Context, sub tierlist.Submission, now time.Time, createOnly bool) (tierlist.SaveResult, error) {
	items := make([]string, len(sub.Placements))
	tiers := make([]string, len(sub.Placements))
	seen := make(map[string]struct{}, len(sub.Placements))
	for i, p := range sub.Placements {
		if _, dup := seen[p.ItemID]; dup {
			return tierlist.SaveResult{}, fmt.Errorf("%w: item %q placed twice", domain.ErrInvalid, p.ItemID)
		}
		seen[p.ItemID] = struct{}{}
		items[i], tiers[i] = p.ItemID, p.TierID
	}

	var res tierlist.SaveResult
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		var status string
		err := tx.QueryRow(ctx, `SELECT status FROM tierlists WHERE id = $1 FOR SHARE`, sub.TierlistID).Scan(&status)
		if errors.Is(err, pgx.ErrNoRows) {
			return tierlist.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("postgres: lock tierlist: %w", err)
		}
		if domain.Status(status) != domain.StatusOpen {
			return tierlist.ErrNotAcceptingSubmissions
		}

		// The conflict update locks an existing row, so concurrent saves of
		// one session serialize. xmax <> 0 marks the update path. The
		// create-only path inserts nothing on conflict, which lets two
		// concurrent first answers of one session resolve to exactly one row.
		if createOnly {
			err = tx.QueryRow(ctx, `
				INSERT INTO submissions (id, tierlist_id, session_id, created_at, updated_at)
				VALUES ($1, $2, $3, $4, $4)
				ON CONFLICT (tierlist_id, session_id) DO NOTHING
				RETURNING id`,
				sub.ID, sub.TierlistID, sub.SessionID, now).Scan(&res.SubmissionID)
			if errors.Is(err, pgx.ErrNoRows) {
				return tierlist.ErrAlreadySubmitted
			}
			if err != nil {
				return mapWriteErr("insert submission", err)
			}
		} else {
			err = tx.QueryRow(ctx, `
				INSERT INTO submissions (id, tierlist_id, session_id, created_at, updated_at)
				VALUES ($1, $2, $3, $4, $4)
				ON CONFLICT (tierlist_id, session_id) DO UPDATE SET updated_at = GREATEST(EXCLUDED.updated_at, submissions.created_at)
				RETURNING id, xmax <> 0`,
				sub.ID, sub.TierlistID, sub.SessionID, now).Scan(&res.SubmissionID, &res.Replaced)
			if err != nil {
				return mapWriteErr("upsert submission", err)
			}
		}

		if res.Replaced {
			// Take the old answer out of the counters before dropping it.
			if _, err := tx.Exec(ctx, `
				UPDATE item_tier_counts c SET count = c.count - 1
				FROM placements p
				WHERE p.submission_id = $1 AND c.tierlist_id = p.tierlist_id
				  AND c.item_id = p.item_id AND c.tier_id = p.tier_id`, res.SubmissionID); err != nil {
				return fmt.Errorf("postgres: decrement counts: %w", err)
			}
			if _, err := tx.Exec(ctx, `DELETE FROM placements WHERE submission_id = $1`, res.SubmissionID); err != nil {
				return fmt.Errorf("postgres: delete placements: %w", err)
			}
		}

		if _, err := tx.Exec(ctx, `
			INSERT INTO placements (submission_id, tierlist_id, item_id, tier_id)
			SELECT $1, $2, * FROM unnest($3::text[], $4::text[])`,
			res.SubmissionID, sub.TierlistID, items, tiers); err != nil {
			return mapWriteErr("insert placements", err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO item_tier_counts (tierlist_id, item_id, tier_id, count)
			SELECT $1, i, t, 1 FROM unnest($2::text[], $3::text[]) AS u(i, t)
			ON CONFLICT (tierlist_id, item_id, tier_id) DO UPDATE SET count = item_tier_counts.count + 1`,
			sub.TierlistID, items, tiers); err != nil {
			return mapWriteErr("increment counts", err)
		}
		return nil
	})
	if err != nil {
		return tierlist.SaveResult{}, err
	}
	return res, nil
}

// Answers implements tierlist.Answers.
func (s *Store) Answers(ctx context.Context, tierlistID string) ([]tierlist.StoredAnswer, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT s.id, p.item_id, t.position
		FROM submissions s
		JOIN placements p ON p.submission_id = s.id
		JOIN tiers t ON t.tierlist_id = p.tierlist_id AND t.id = p.tier_id
		WHERE s.tierlist_id = $1
		ORDER BY s.created_at, s.id`, tierlistID)
	if err != nil {
		return nil, fmt.Errorf("postgres: query answers: %w", err)
	}
	defer rows.Close()

	var out []tierlist.StoredAnswer
	for rows.Next() {
		var id, item string
		var pos int
		if err := rows.Scan(&id, &item, &pos); err != nil {
			return nil, fmt.Errorf("postgres: scan answer: %w", err)
		}
		if len(out) == 0 || out[len(out)-1].ID != id {
			out = append(out, tierlist.StoredAnswer{ID: id, Answer: similarity.Answer{}})
		}
		out[len(out)-1].Answer[similarity.ItemID(item)] = pos
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: query answers: %w", err)
	}
	return out, nil
}

// GetSubmissionByID implements tierlist.Answers.
func (s *Store) GetSubmissionByID(ctx context.Context, id string) (*tierlist.StoredSubmission, error) {
	return s.getSubmission(ctx, "s.id = $1", id)
}

// GetSubmissionBySession implements tierlist.Answers.
func (s *Store) GetSubmissionBySession(ctx context.Context, tierlistID, sessionID string) (*tierlist.StoredSubmission, error) {
	return s.getSubmission(ctx, "s.tierlist_id = $1 AND s.session_id = $2", tierlistID, sessionID)
}

// ListSubmissionsBySession implements tierlist.Answers.
func (s *Store) ListSubmissionsBySession(ctx context.Context, sessionID string) ([]tierlist.StoredSubmission, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT s.id, s.tierlist_id, tl.slug, s.session_id, s.created_at, s.updated_at,
		       p.item_id, p.tier_id
		FROM submissions s
		JOIN tierlists tl ON tl.id = s.tierlist_id
		LEFT JOIN placements p ON p.submission_id = s.id
		WHERE s.session_id = $1
		ORDER BY s.updated_at DESC, s.id, p.item_id`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("postgres: query session submissions: %w", err)
	}
	defer rows.Close()
	return scanSubmissions(rows)
}

// getSubmission loads one answer with its placements.
func (s *Store) getSubmission(ctx context.Context, where string, args ...any) (*tierlist.StoredSubmission, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT s.id, s.tierlist_id, tl.slug, s.session_id, s.created_at, s.updated_at,
		       p.item_id, p.tier_id
		FROM submissions s
		JOIN tierlists tl ON tl.id = s.tierlist_id
		LEFT JOIN placements p ON p.submission_id = s.id
		WHERE `+where+`
		ORDER BY p.item_id`, args...)
	if err != nil {
		return nil, fmt.Errorf("postgres: query submission: %w", err)
	}
	defer rows.Close()
	out, err := scanSubmissions(rows)
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, tierlist.ErrNotFound
	}
	return &out[0], nil
}

// scanSubmissions groups joined submission rows (one row per placement) into
// answers. The query must order rows by submission first.
func scanSubmissions(rows pgx.Rows) ([]tierlist.StoredSubmission, error) {
	var out []tierlist.StoredSubmission
	for rows.Next() {
		var (
			id, tierlistID, slug, sessionID string
			created, updated                time.Time
			itemID, tierID                  *string
		)
		if err := rows.Scan(&id, &tierlistID, &slug, &sessionID, &created, &updated, &itemID, &tierID); err != nil {
			return nil, fmt.Errorf("postgres: scan submission: %w", err)
		}
		if len(out) == 0 || out[len(out)-1].ID != id {
			out = append(out, tierlist.StoredSubmission{
				ID: id, TierlistID: tierlistID, TierlistSlug: slug, SessionID: sessionID,
				CreatedAt: created, UpdatedAt: updated,
			})
		}
		if itemID != nil && tierID != nil {
			out[len(out)-1].Placements = append(out[len(out)-1].Placements,
				tierlist.Placement{ItemID: *itemID, TierID: *tierID})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: query submissions: %w", err)
	}
	return out, nil
}

// Distributions implements tierlist.Answers.
func (s *Store) Distributions(ctx context.Context, tierlistID string) ([]tierlist.ItemDistribution, error) {
	var tiers int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM tiers WHERE tierlist_id = $1`, tierlistID).Scan(&tiers); err != nil {
		return nil, fmt.Errorf("postgres: count tiers: %w", err)
	}
	if tiers == 0 {
		return nil, tierlist.ErrNotFound
	}

	rows, err := s.pool.Query(ctx, `
		SELECT i.id, t.position, COALESCE(c.count, 0)
		FROM items i
		CROSS JOIN tiers t
		LEFT JOIN item_tier_counts c
		  ON c.tierlist_id = i.tierlist_id AND c.item_id = i.id AND c.tier_id = t.id
		WHERE i.tierlist_id = $1 AND t.tierlist_id = $1
		ORDER BY i.position, t.position`, tierlistID)
	if err != nil {
		return nil, fmt.Errorf("postgres: query distributions: %w", err)
	}
	defer rows.Close()

	var out []tierlist.ItemDistribution
	for rows.Next() {
		var id string
		var pos, n int
		if err := rows.Scan(&id, &pos, &n); err != nil {
			return nil, fmt.Errorf("postgres: scan distribution: %w", err)
		}
		if len(out) == 0 || out[len(out)-1].ItemID != id {
			out = append(out, tierlist.ItemDistribution{ItemID: id, Counts: make([]int, tiers)})
		}
		out[len(out)-1].Counts[pos] = n
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: query distributions: %w", err)
	}
	return out, nil
}
