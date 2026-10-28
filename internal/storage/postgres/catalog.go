package postgres

import (
	"context"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"

	"code.sarnas.ru/tierl/internal/tierlist"
)

// ListPublic implements tierlist.Store. It returns only public, open tier
// lists, applies the optional title/description search, and uses keyset
// pagination: (created_at, id) for "new" and (submissions_count, created_at,
// id) for "popular".
func (s *Store) ListPublic(ctx context.Context, p tierlist.PageParams) (tierlist.CatalogPage, error) {
	limit := normalizeLimit(p.Limit)
	sort := strings.ToLower(strings.TrimSpace(p.Sort))
	switch sort {
	case "":
		sort = tierlist.CatalogSortNew
	case tierlist.CatalogSortNew, tierlist.CatalogSortPopular:
	default:
		return tierlist.CatalogPage{}, tierlist.ErrBadSort
	}

	var search *string
	if p.Query != "" {
		escaped := escapeLike(p.Query)
		search = &escaped
	}

	var cur catalogCursor
	hasCursor := false
	if p.Cursor != "" {
		decoded, err := decodeCursor(p.Cursor, sort)
		if err != nil {
			return tierlist.CatalogPage{}, err
		}
		cur, hasCursor = decoded, true
	}

	var (
		rows queryRows
		err  error
	)
	if sort == tierlist.CatalogSortPopular {
		var afterCount *int64
		var afterTime *time.Time
		afterID := ""
		if hasCursor {
			n := cur.submissions
			afterCount = &n
			afterTime = &cur.createdAt
			afterID = cur.id
		}
		rows, err = s.pool.Query(ctx, `
			SELECT c.id, c.author_id, c.slug, c.title, c.description, c.created_at,
			       c.item_count, c.submissions_count
			FROM (
				SELECT t.id, t.author_id, t.slug, t.title, t.description, t.created_at,
				       (SELECT count(*) FROM items i WHERE i.tierlist_id = t.id) AS item_count,
				       (SELECT count(*) FROM submissions s WHERE s.tierlist_id = t.id) AS submissions_count
				FROM tierlists t
				WHERE t.visibility = 'public' AND t.status = 'open' AND t.hidden_at IS NULL
				  AND ($1::text IS NULL OR (t.title ILIKE '%' || $1 || '%' ESCAPE '\'
				       OR t.description ILIKE '%' || $1 || '%' ESCAPE '\'))
			) c
			WHERE ($2::bigint IS NULL OR (c.submissions_count, c.created_at, c.id) < ($2::bigint, $3::timestamptz, $4))
			ORDER BY c.submissions_count DESC, c.created_at DESC, c.id DESC
			LIMIT $5`, search, afterCount, afterTime, afterID, limit+1)
	} else {
		var afterTime *time.Time
		afterID := ""
		if hasCursor {
			afterTime = &cur.createdAt
			afterID = cur.id
		}
		rows, err = s.pool.Query(ctx, `
			SELECT t.id, t.author_id, t.slug, t.title, t.description, t.created_at,
			       (SELECT count(*) FROM items i WHERE i.tierlist_id = t.id),
			       (SELECT count(*) FROM submissions s WHERE s.tierlist_id = t.id)
			FROM tierlists t
			WHERE t.visibility = 'public' AND t.status = 'open' AND t.hidden_at IS NULL
			  AND ($1::text IS NULL OR (t.title ILIKE '%' || $1 || '%' ESCAPE '\'
			       OR t.description ILIKE '%' || $1 || '%' ESCAPE '\'))
			  AND ($2::timestamptz IS NULL OR (t.created_at, t.id) < ($2, $3))
			ORDER BY t.created_at DESC, t.id DESC
			LIMIT $4`, search, afterTime, afterID, limit+1)
	}
	if err != nil {
		return tierlist.CatalogPage{}, fmt.Errorf("postgres: list public: %w", err)
	}
	defer rows.Close()

	var page tierlist.CatalogPage
	for rows.Next() {
		var e tierlist.CatalogEntry
		if err := rows.Scan(&e.ID, &e.AuthorID, &e.Slug, &e.Title, &e.Description, &e.CreatedAt, &e.ItemCount, &e.Submissions); err != nil {
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
		if sort == tierlist.CatalogSortPopular {
			page.NextCursor = encodePopularCursor(last.Submissions, last.CreatedAt, last.ID)
		} else {
			page.NextCursor = encodeCursor(last.CreatedAt, last.ID)
		}
	}
	return page, nil
}

// queryRows is the subset of pgx.Rows used by ListPublic.
type queryRows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
	Close()
}

func normalizeLimit(limit int) int {
	switch {
	case limit <= 0:
		return tierlist.DefaultPageSize
	case limit > tierlist.MaxPageSize:
		return tierlist.MaxPageSize
	default:
		return limit
	}
}

func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

type catalogCursor struct {
	createdAt   time.Time
	id          string
	submissions int64
}

func encodeCursor(ts time.Time, id string) string {
	raw := strconv.FormatInt(ts.UnixMicro(), 10) + "|" + id
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func encodePopularCursor(submissions int, ts time.Time, id string) string {
	raw := strconv.FormatInt(ts.UnixMicro(), 10) + "|" + id + "|" + strconv.Itoa(submissions)
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeCursor(c, sort string) (catalogCursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(c)
	if err != nil {
		return catalogCursor{}, tierlist.ErrBadCursor
	}
	parts := strings.Split(string(raw), "|")
	if sort == tierlist.CatalogSortPopular {
		if len(parts) != 3 || parts[1] == "" {
			return catalogCursor{}, tierlist.ErrBadCursor
		}
		ts, err := parseCursorTime(parts[0])
		if err != nil {
			return catalogCursor{}, err
		}
		n, err := strconv.ParseInt(parts[2], 10, 64)
		if err != nil || n < 0 {
			return catalogCursor{}, tierlist.ErrBadCursor
		}
		return catalogCursor{createdAt: ts, id: parts[1], submissions: n}, nil
	}
	if len(parts) != 2 || parts[1] == "" {
		return catalogCursor{}, tierlist.ErrBadCursor
	}
	ts, err := parseCursorTime(parts[0])
	if err != nil {
		return catalogCursor{}, err
	}
	return catalogCursor{createdAt: ts, id: parts[1]}, nil
}

func parseCursorTime(raw string) (time.Time, error) {
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return time.Time{}, tierlist.ErrBadCursor
	}
	return time.UnixMicro(n).UTC(), nil
}
