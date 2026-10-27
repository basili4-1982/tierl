package postgres

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"

	"code.sarnas.ru/tierl/internal/domain"
	"code.sarnas.ru/tierl/internal/tierlist"
)

// Create implements tierlist.Store.
func (s *Store) Create(ctx context.Context, t *domain.Tierlist) error {
	return s.inTx(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO tierlists (id, slug, author_id, title, description, visibility, status, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			t.ID, t.Slug, t.AuthorID, t.Title, t.Description, string(t.Visibility), string(t.Status), t.CreatedAt, t.UpdatedAt)
		if err != nil {
			return mapWriteErr("create tierlist", err)
		}
		return insertStructure(ctx, tx, t)
	})
}

func insertStructure(ctx context.Context, tx pgx.Tx, t *domain.Tierlist) error {
	var (
		ids, names, colors []string
		positions          []int32
	)
	for _, tr := range t.Tiers {
		ids, names, colors, positions = append(ids, tr.ID), append(names, tr.Name), append(colors, tr.Color), append(positions, int32(tr.Position)) //nolint:gosec // bounded by domain limits
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO tiers (tierlist_id, id, name, color, position)
		SELECT $1, * FROM unnest($2::text[], $3::text[], $4::text[], $5::int[])`,
		t.ID, ids, names, colors, positions); err != nil {
		return mapWriteErr("insert tiers", err)
	}

	var labels []string
	var urls []*string
	ids, positions = nil, nil
	for _, it := range t.Items {
		ids, labels, positions = append(ids, it.ID), append(labels, it.Label), append(positions, int32(it.Position)) //nolint:gosec // bounded by domain limits
		if it.ImageURL == "" {
			urls = append(urls, nil)
		} else {
			urls = append(urls, &it.ImageURL)
		}
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO items (tierlist_id, id, label, image_url, position)
		SELECT $1, * FROM unnest($2::text[], $3::text[], $4::text[], $5::int[])`,
		t.ID, ids, labels, urls, positions); err != nil {
		return mapWriteErr("insert items", err)
	}
	return nil
}

// GetBySlug implements tierlist.Store.
func (s *Store) GetBySlug(ctx context.Context, slug string) (*domain.Tierlist, error) {
	return s.get(ctx, s.pool, "slug", slug)
}

// GetByID implements tierlist.Store.
func (s *Store) GetByID(ctx context.Context, id string) (*domain.Tierlist, error) {
	return s.get(ctx, s.pool, "id", id)
}

type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// get loads a tier list by a column that is "id" or "slug" (never user input).
func (s *Store) get(ctx context.Context, q querier, column, value string) (*domain.Tierlist, error) {
	var (
		t          domain.Tierlist
		vis, state string
	)
	err := q.QueryRow(ctx, `
		SELECT id, slug, author_id, title, description, visibility, status, created_at, updated_at
		FROM tierlists WHERE `+column+` = $1`, value).
		Scan(&t.ID, &t.Slug, &t.AuthorID, &t.Title, &t.Description, &vis, &state, &t.CreatedAt, &t.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, tierlist.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("postgres: get tierlist: %w", err)
	}
	t.Visibility, t.Status = domain.Visibility(vis), domain.Status(state)
	if err := loadStructure(ctx, q, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

func loadStructure(ctx context.Context, q querier, t *domain.Tierlist) error {
	rows, err := q.Query(ctx, `SELECT id, name, color, position FROM tiers WHERE tierlist_id = $1 ORDER BY position`, t.ID)
	if err != nil {
		return fmt.Errorf("postgres: load tiers: %w", err)
	}
	t.Tiers, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (domain.Tier, error) {
		var tr domain.Tier
		err := r.Scan(&tr.ID, &tr.Name, &tr.Color, &tr.Position)
		return tr, err
	})
	if err != nil {
		return fmt.Errorf("postgres: load tiers: %w", err)
	}

	rows, err = q.Query(ctx, `SELECT id, label, COALESCE(image_url, ''), position FROM items WHERE tierlist_id = $1 ORDER BY position`, t.ID)
	if err != nil {
		return fmt.Errorf("postgres: load items: %w", err)
	}
	t.Items, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (domain.Item, error) {
		var it domain.Item
		err := r.Scan(&it.ID, &it.Label, &it.ImageURL, &it.Position)
		return it, err
	})
	if err != nil {
		return fmt.Errorf("postgres: load items: %w", err)
	}
	return nil
}

// Update implements tierlist.Store.
func (s *Store) Update(ctx context.Context, t *domain.Tierlist) error {
	return s.inTx(ctx, func(tx pgx.Tx) error {
		// Lock the row so concurrent answers/updates serialize with the structure check.
		var locked string
		err := tx.QueryRow(ctx, `SELECT id FROM tierlists WHERE id = $1 FOR UPDATE`, t.ID).Scan(&locked)
		if errors.Is(err, pgx.ErrNoRows) {
			return tierlist.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("postgres: lock tierlist: %w", err)
		}

		current := &domain.Tierlist{ID: t.ID}
		if err := loadStructure(ctx, tx, current); err != nil {
			return err
		}
		if !slices.Equal(current.Items, t.Items) || !slices.Equal(current.Tiers, t.Tiers) {
			var n int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM submissions WHERE tierlist_id = $1`, t.ID).Scan(&n); err != nil {
				return fmt.Errorf("postgres: count submissions: %w", err)
			}
			if n > 0 {
				return domain.ErrStructureLocked
			}
			if _, err := tx.Exec(ctx, `DELETE FROM items WHERE tierlist_id = $1`, t.ID); err != nil {
				return fmt.Errorf("postgres: delete items: %w", err)
			}
			if _, err := tx.Exec(ctx, `DELETE FROM tiers WHERE tierlist_id = $1`, t.ID); err != nil {
				return fmt.Errorf("postgres: delete tiers: %w", err)
			}
			if err := insertStructure(ctx, tx, t); err != nil {
				return err
			}
		}

		_, err = tx.Exec(ctx, `
			UPDATE tierlists
			SET slug = $2, title = $3, description = $4, visibility = $5, status = $6, updated_at = $7
			WHERE id = $1`,
			t.ID, t.Slug, t.Title, t.Description, string(t.Visibility), string(t.Status), t.UpdatedAt)
		return mapWriteErr("update tierlist", err)
	})
}

// SetStatus implements tierlist.Store.
func (s *Store) SetStatus(ctx context.Context, id string, status domain.Status, now time.Time) error {
	tag, err := s.pool.Exec(ctx, `UPDATE tierlists SET status = $2, updated_at = GREATEST($3, created_at) WHERE id = $1`,
		id, string(status), now)
	if err != nil {
		return mapWriteErr("set status", err)
	}
	if tag.RowsAffected() == 0 {
		return tierlist.ErrNotFound
	}
	return nil
}

// Delete implements tierlist.Store.
func (s *Store) Delete(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM tierlists WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("postgres: delete tierlist: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return tierlist.ErrNotFound
	}
	return nil
}

// CountSubmissions implements tierlist.Store.
func (s *Store) CountSubmissions(ctx context.Context, id string) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `
		SELECT count(s.id) FROM tierlists t LEFT JOIN submissions s ON s.tierlist_id = t.id
		WHERE t.id = $1 GROUP BY t.id`, id).Scan(&n)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, tierlist.ErrNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("postgres: count submissions: %w", err)
	}
	return n, nil
}

// mapWriteErr translates constraint violations to typed errors.
func mapWriteErr(op string, err error) error {
	if err == nil {
		return nil
	}
	if isSlugCollision(err) {
		return tierlist.ErrSlugTaken
	}
	if pe := pgErr(err); pe != nil {
		switch pe.Code {
		case codeCheckViolation, codeUniqueViolation, codeForeignKeyViolation:
			return fmt.Errorf("%w: %s: constraint %s", domain.ErrInvalid, op, pe.ConstraintName)
		}
	}
	return fmt.Errorf("postgres: %s: %w", op, err)
}
