package postgres

import (
	"context"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/basili4-1982/tierl/internal/tierlist"
)

// ListPublic implements tierlist.Store with keyset pagination on (created_at, id).
func (s *Store) ListPublic(ctx context.Context, p tierlist.PageParams) (tierlist.CatalogPage, error) {
	limit := p.Limit
	switch {
	case limit <= 0:
		limit = tierlist.DefaultPageSize
	case limit > tierlist.MaxPageSize:
		limit = tierlist.MaxPageSize
	}

	var (
		after   *time.Time
		afterID string
	)
	if p.Cursor != "" {
		ts, id, err := decodeCursor(p.Cursor)
		if err != nil {
			return tierlist.CatalogPage{}, err
		}
		after, afterID = &ts, id
	}

	// The (created_at, id) predicate matches the partial index tierlists_catalog_idx.
	rows, err := s.pool.Query(ctx, `
		SELECT t.id, t.slug, t.title, t.description, t.created_at,
		       (SELECT count(*) FROM items i WHERE i.tierlist_id = t.id),
		       (SELECT count(*) FROM submissions s WHERE s.tierlist_id = t.id)
		FROM tierlists t
		WHERE t.visibility = 'public' AND t.status = 'open'
		  AND ($1::timestamptz IS NULL OR (t.created_at, t.id) < ($1, $2))
		ORDER BY t.created_at DESC, t.id DESC
		LIMIT $3`, after, afterID, limit+1)
	if err != nil {
		return tierlist.CatalogPage{}, fmt.Errorf("postgres: list public: %w", err)
	}
	defer rows.Close()

	var page tierlist.CatalogPage
	for rows.Next() {
		var e tierlist.CatalogEntry
		if err := rows.Scan(&e.ID, &e.Slug, &e.Title, &e.Description, &e.CreatedAt, &e.ItemCount, &e.Submissions); err != nil {
			return tierlist.CatalogPage{}, fmt.Errorf("postgres: scan catalog: %w", err)
		}
		page.Entries = append(page.Entries, e)
	}
	if err := rows.Err(); err != nil {
		return tierlist.CatalogPage{}, fmt.Errorf("postgres: list public: %w", err)
	}
	if len(page.Entries) > limit {
		page.Entries = page.Entries[:limit]
		last := page.Entries[limit-1]
		page.NextCursor = encodeCursor(last.CreatedAt, last.ID)
	}
	return page, nil
}

func encodeCursor(ts time.Time, id string) string {
	raw := strconv.FormatInt(ts.UnixMicro(), 10) + "|" + id
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeCursor(c string) (time.Time, string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(c)
	if err != nil {
		return time.Time{}, "", tierlist.ErrBadCursor
	}
	micro, id, ok := strings.Cut(string(raw), "|")
	if !ok || id == "" {
		return time.Time{}, "", tierlist.ErrBadCursor
	}
	n, err := strconv.ParseInt(micro, 10, 64)
	if err != nil {
		return time.Time{}, "", tierlist.ErrBadCursor
	}
	return time.UnixMicro(n).UTC(), id, nil
}
