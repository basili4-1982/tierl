package api

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"code.sarnas.ru/tierl/internal/tierlist"
)

type catalogSummaryDTO struct {
	ID               string    `json:"id"`
	Slug             string    `json:"slug"`
	Title            string    `json:"title"`
	Description      string    `json:"description"`
	Visibility       string    `json:"visibility"`
	Status           string    `json:"status"`
	ItemsCount       int       `json:"items_count"`
	SubmissionsCount int       `json:"submissions_count"`
	CoverURL         *string   `json:"cover_url"`
	Author           authorDTO `json:"author"`
	CreatedAt        time.Time `json:"created_at"`
}

type catalogPageDTO struct {
	Items      []catalogSummaryDTO `json:"items"`
	NextCursor *string             `json:"next_cursor"`
}

// listCatalog implements GET /api/v1/catalog.
func (a *API) listCatalog(w http.ResponseWriter, r *http.Request) {
	params, err := parseCatalogParams(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error(), nil)
		return
	}
	page, err := a.listPublic(r.Context(), params)
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	items := make([]catalogSummaryDTO, 0, len(page.Entries))
	for _, e := range page.Entries {
		items = append(items, catalogSummaryDTO{
			ID:               e.ID,
			Slug:             e.Slug,
			Title:            e.Title,
			Description:      e.Description,
			Visibility:       "public",
			Status:           "open",
			ItemsCount:       e.ItemCount,
			SubmissionsCount: e.Submissions,
			Author:           authorDTO{ID: e.AuthorID, DisplayName: a.authorName(r.Context(), e.AuthorID)},
			CreatedAt:        e.CreatedAt.UTC(),
		})
	}
	var next *string
	if page.NextCursor != "" {
		c := page.NextCursor
		next = &c
	}
	writeJSON(w, http.StatusOK, catalogPageDTO{Items: items, NextCursor: next})
}

func parseCatalogParams(r *http.Request) (tierlist.PageParams, error) {
	q := r.URL.Query()
	params := tierlist.PageParams{
		Sort:   strings.ToLower(strings.TrimSpace(q.Get("sort"))),
		Cursor: q.Get("cursor"),
	}
	switch params.Sort {
	case "":
		params.Sort = tierlist.CatalogSortNew
	case tierlist.CatalogSortNew, tierlist.CatalogSortPopular:
	default:
		return tierlist.PageParams{}, fmt.Errorf("неизвестная сортировка")
	}
	if len(params.Cursor) > 512 {
		return tierlist.PageParams{}, fmt.Errorf("слишком длинный курсор")
	}
	if values, ok := q["q"]; ok {
		raw := ""
		if len(values) > 0 {
			raw = values[0]
		}
		if utf8.RuneCountInString(raw) > 100 {
			return tierlist.PageParams{}, fmt.Errorf("поисковый запрос длиннее 100 символов")
		}
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return tierlist.PageParams{}, fmt.Errorf("поисковый запрос не может быть пустым")
		}
		params.Query = raw
	}
	params.Limit = tierlist.DefaultPageSize
	if raw := strings.TrimSpace(q.Get("limit")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			return tierlist.PageParams{}, fmt.Errorf("некорректный размер страницы")
		}
		switch {
		case n <= 0:
			n = tierlist.DefaultPageSize
		case n > tierlist.MaxPageSize:
			n = tierlist.MaxPageSize
		}
		params.Limit = n
	}
	return params, nil
}

// listPublic calls the service when it is wired and falls back to the store
// used by the share pages otherwise.
func (a *API) listPublic(ctx context.Context, p tierlist.PageParams) (tierlist.CatalogPage, error) {
	if a.tierlists != nil {
		return a.tierlists.ListPublic(ctx, p)
	}
	if a.store != nil {
		return a.store.ListPublic(ctx, p)
	}
	return tierlist.CatalogPage{}, nil
}

func (a *API) allPublicEntries(ctx context.Context) ([]tierlist.CatalogEntry, error) {
	var out []tierlist.CatalogEntry
	cursor := ""
	for {
		page, err := a.listPublic(ctx, tierlist.PageParams{
			Limit:  tierlist.MaxPageSize,
			Cursor: cursor,
			Sort:   tierlist.CatalogSortNew,
		})
		if err != nil {
			return nil, err
		}
		out = append(out, page.Entries...)
		if page.NextCursor == "" {
			return out, nil
		}
		cursor = page.NextCursor
	}
}

// sitemap serves GET /sitemap.xml with only public, open tier list pages.
func (a *API) sitemap(w http.ResponseWriter, r *http.Request) {
	entries, err := a.allPublicEntries(r.Context())
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	base := publicBaseURL(r)
	var b bytes.Buffer
	b.WriteString(xml.Header)
	b.WriteString(`<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">` + "\n")
	for _, e := range entries {
		b.WriteString("<url><loc>")
		_ = xml.EscapeText(&b, []byte(base+"/t/"+e.Slug))
		b.WriteString("</loc><lastmod>")
		b.WriteString(e.CreatedAt.UTC().Format(time.RFC3339))
		b.WriteString("</lastmod></url>\n")
	}
	b.WriteString("</urlset>\n")

	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.Header().Set("Content-Length", strconv.Itoa(b.Len()))
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(b.Bytes())
}
