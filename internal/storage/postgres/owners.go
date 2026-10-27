package postgres

import (
	"context"
	"fmt"
	"time"

	"code.sarnas.ru/tierl/internal/domain"
	"code.sarnas.ru/tierl/internal/tierlist"
)

var _ tierlist.OwnerStore = (*Store)(nil)

// ListByOwner implements tierlist.OwnerStore. The author index covers the
// lookup and both aggregates come from the same query, so a profile page is
// one round trip instead of N+1.
func (s *Store) ListByOwner(ctx context.Context, userID string) ([]tierlist.OwnerEntry, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT t.id, t.slug, t.author_id, t.title, t.description,
		       t.visibility, t.status, t.created_at,
		       (SELECT count(*) FROM items i WHERE i.tierlist_id = t.id),
		       (SELECT count(*) FROM submissions s WHERE s.tierlist_id = t.id)
		FROM tierlists t
		WHERE t.author_id = $1
		ORDER BY t.created_at DESC, t.id DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("postgres: list by owner: %w", err)
	}
	defer rows.Close()

	var out []tierlist.OwnerEntry
	for rows.Next() {
		var (
			e          tierlist.OwnerEntry
			visibility string
			status     string
		)
		if err := rows.Scan(&e.ID, &e.Slug, &e.AuthorID, &e.Title, &e.Description,
			&visibility, &status, &e.CreatedAt, &e.ItemCount, &e.Submissions); err != nil {
			return nil, fmt.Errorf("postgres: scan owner list: %w", err)
		}
		e.Visibility, e.Status = domain.Visibility(visibility), domain.Status(status)
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: list by owner: %w", err)
	}
	return out, nil
}

// SetStatusForOwner implements tierlist.OwnerStore.
func (s *Store) SetStatusForOwner(ctx context.Context, id, userID string, status domain.Status, now time.Time) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE tierlists SET status = $3, updated_at = GREATEST($4, created_at)
		WHERE id = $1 AND author_id = $2`, id, userID, string(status), now)
	if err != nil {
		return mapWriteErr("set owner status", err)
	}
	if tag.RowsAffected() == 0 {
		return tierlist.ErrNotFound
	}
	return nil
}

// DeleteForOwner implements tierlist.OwnerStore.
func (s *Store) DeleteForOwner(ctx context.Context, id, userID string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM tierlists WHERE id = $1 AND author_id = $2`, id, userID)
	if err != nil {
		return fmt.Errorf("postgres: delete owner tierlist: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return tierlist.ErrNotFound
	}
	return nil
}
