package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"code.sarnas.ru/tierl/internal/storage/postgres"
	"code.sarnas.ru/tierl/internal/tierlist"
	"code.sarnas.ru/tierl/internal/web/api"
)

// ogResultReader resolves a participant result for server-rendered share
// pages. It only reads; the submission HTTP API owns the full result flow.
type ogResultReader struct {
	pool  *pgxpool.Pool
	store *postgres.Store
	users tierlist.Users
}

var _ api.ResultProvider = ogResultReader{}

func (r ogResultReader) GetOGResult(ctx context.Context, id string) (*api.OGResult, error) {
	var tierlistID string
	err := r.pool.QueryRow(ctx, `SELECT tierlist_id FROM submissions WHERE id = $1`, id).Scan(&tierlistID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, tierlist.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("og result: load submission: %w", err)
	}

	tl, err := r.store.GetByID(ctx, tierlistID)
	if err != nil {
		return nil, err
	}
	rows, err := r.pool.Query(ctx, `
		SELECT item_id, tier_id FROM placements
		WHERE submission_id = $1 ORDER BY item_id`, id)
	if err != nil {
		return nil, fmt.Errorf("og result: load placements: %w", err)
	}
	placements, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (tierlist.Placement, error) {
		var p tierlist.Placement
		err := row.Scan(&p.ItemID, &p.TierID)
		return p, err
	})
	if err != nil {
		return nil, fmt.Errorf("og result: scan placements: %w", err)
	}

	res := &api.OGResult{ID: id, Tierlist: tl, Placements: placements}
	if r.users != nil {
		if prof, err := r.users.Profile(ctx, tl.AuthorID); err == nil {
			res.AuthorName = prof.DisplayName
		} else if !errors.Is(err, tierlist.ErrNotFound) {
			return nil, fmt.Errorf("og result: load author: %w", err)
		}
	}
	return res, nil
}
