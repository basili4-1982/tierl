package api_test

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"code.sarnas.ru/tierl/internal/tierlist"
	"code.sarnas.ru/tierl/internal/tierlist/tierlisttest"
	"code.sarnas.ru/tierl/internal/web"
	"code.sarnas.ru/tierl/internal/web/api"
)

type catalogStore struct {
	*tierlisttest.MemStore
	page  tierlist.CatalogPage
	pages map[string]tierlist.CatalogPage
	err   error
	calls []tierlist.PageParams
}

func (s *catalogStore) ListPublic(_ context.Context, p tierlist.PageParams) (tierlist.CatalogPage, error) {
	s.calls = append(s.calls, p)
	if s.err != nil {
		return tierlist.CatalogPage{}, s.err
	}
	if s.pages != nil {
		return s.pages[p.Cursor], nil
	}
	return s.page, nil
}

const catalogAuthorID = "00000000-0000-4000-8000-000000000009"

func newCatalogAPI(store tierlist.Store) http.Handler {
	return api.New(api.Deps{
		Store: store,
		Auth:  headerAuth{},
		Log:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		Users: tierlisttest.Users{catalogAuthorID: {DisplayName: "Anna", EmailVerified: true}},
	})
}

func catalogEntry(id, slug string, submissions int) tierlist.CatalogEntry {
	return tierlist.CatalogEntry{
		ID: id, AuthorID: catalogAuthorID, Slug: slug, Title: "Опрос " + id,
		Description: "Описание", ItemCount: 3, Submissions: submissions,
		CreatedAt: time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC),
	}
}

func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestCatalogPage(t *testing.T) {
	store := &catalogStore{
		MemStore: tierlisttest.NewMemStore(),
		page: tierlist.CatalogPage{
			Entries:    []tierlist.CatalogEntry{catalogEntry("a", "alpha-list", 4)},
			NextCursor: "next",
		},
	}
	rec := get(t, newCatalogAPI(store), "/api/v1/catalog?limit=2&sort=popular&q=go")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if len(store.calls) != 1 {
		t.Fatalf("ListPublic calls = %d", len(store.calls))
	}
	got := store.calls[0]
	if got.Limit != 2 || got.Sort != tierlist.CatalogSortPopular || got.Query != "go" {
		t.Errorf("params = %+v", got)
	}
	var page struct {
		Items []struct {
			ID          string `json:"id"`
			Slug        string `json:"slug"`
			Title       string `json:"title"`
			Visibility  string `json:"visibility"`
			Status      string `json:"status"`
			ItemsCount  int    `json:"items_count"`
			Submissions int    `json:"submissions_count"`
			Author      struct {
				ID          string `json:"id"`
				DisplayName string `json:"display_name"`
			} `json:"author"`
			CreatedAt string `json:"created_at"`
		} `json:"items"`
		NextCursor *string `json:"next_cursor"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].ID != "a" || page.Items[0].Visibility != "public" ||
		page.Items[0].Status != "open" || page.Items[0].ItemsCount != 3 ||
		page.Items[0].Submissions != 4 || page.Items[0].Author.ID != catalogAuthorID ||
		page.Items[0].Author.DisplayName != "Anna" {
		t.Fatalf("items = %+v", page.Items)
	}
	if page.NextCursor == nil || *page.NextCursor != "next" {
		t.Errorf("next_cursor = %v", page.NextCursor)
	}
}

func TestCatalogEmpty(t *testing.T) {
	store := &catalogStore{MemStore: tierlisttest.NewMemStore()}
	rec := get(t, newCatalogAPI(store), "/api/v1/catalog")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if items, ok := body["items"].([]any); !ok || len(items) != 0 {
		t.Errorf("items = %v, want []", body["items"])
	}
	if body["next_cursor"] != nil {
		t.Errorf("next_cursor = %v, want null", body["next_cursor"])
	}
}

func TestCatalogBadRequests(t *testing.T) {
	longQ := strings.Repeat("я", 101)
	tests := []struct {
		name  string
		path  string
		err   error
		calls int
	}{
		{name: "unknown sort", path: "/api/v1/catalog?sort=old", calls: 0},
		{name: "empty q", path: "/api/v1/catalog?q=", calls: 0},
		{name: "long q", path: "/api/v1/catalog?q=" + longQ, calls: 0},
		{name: "bad limit", path: "/api/v1/catalog?limit=abc", calls: 0},
		{name: "long cursor", path: "/api/v1/catalog?cursor=" + strings.Repeat("a", 513), calls: 0},
		{name: "bad cursor from store", path: "/api/v1/catalog?cursor=bad", err: tierlist.ErrBadCursor, calls: 1},
		{name: "bad sort from store", path: "/api/v1/catalog", err: tierlist.ErrBadSort, calls: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &catalogStore{MemStore: tierlisttest.NewMemStore(), err: tt.err}
			rec := get(t, newCatalogAPI(store), tt.path)
			wantError(t, rec, http.StatusBadRequest, "bad_request")
			if len(store.calls) != tt.calls {
				t.Errorf("ListPublic calls = %d, want %d", len(store.calls), tt.calls)
			}
		})
	}
}

func TestCatalogClampsPageSize(t *testing.T) {
	tests := map[string]int{"0": tierlist.DefaultPageSize, "1000": tierlist.MaxPageSize}
	for raw, want := range tests {
		store := &catalogStore{MemStore: tierlisttest.NewMemStore()}
		rec := get(t, newCatalogAPI(store), "/api/v1/catalog?limit="+raw)
		if rec.Code != http.StatusOK {
			t.Fatalf("limit %s: %d %s", raw, rec.Code, rec.Body.String())
		}
		if len(store.calls) != 1 {
			t.Fatalf("ListPublic calls = %d", len(store.calls))
		}
		if got := store.calls[0].Limit; got != want {
			t.Errorf("limit %s passed as %d, want %d", raw, got, want)
		}
	}
}

func TestSitemapOnlyPublicPages(t *testing.T) {
	store := &catalogStore{
		MemStore: tierlisttest.NewMemStore(),
		pages: map[string]tierlist.CatalogPage{
			"": {Entries: []tierlist.CatalogEntry{
				catalogEntry("a", "alpha-list", 0),
				catalogEntry("b", "beta-list", 0),
			}, NextCursor: "next"},
			"next": {Entries: []tierlist.CatalogEntry{catalogEntry("c", "gamma-list", 0)}},
		},
	}
	req := httptest.NewRequest(http.MethodGet, "http://example.com/sitemap.xml", nil)
	rec := httptest.NewRecorder()
	newCatalogAPI(store).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/xml") {
		t.Errorf("Content-Type = %q", ct)
	}
	var doc struct {
		URLs []struct {
			Loc string `xml:"loc"`
		} `xml:"url"`
	}
	if err := xml.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("invalid XML: %v\n%s", err, rec.Body.String())
	}
	var locs []string
	for _, u := range doc.URLs {
		locs = append(locs, u.Loc)
	}
	want := []string{
		"http://example.com/t/alpha-list",
		"http://example.com/t/beta-list",
		"http://example.com/t/gamma-list",
	}
	if strings.Join(locs, ",") != strings.Join(want, ",") {
		t.Fatalf("locs = %v, want %v", locs, want)
	}
	if strings.Contains(rec.Body.String(), "unlisted") {
		t.Error("sitemap contains an unlisted URL")
	}
}

func TestSitemapEmpty(t *testing.T) {
	store := &catalogStore{MemStore: tierlisttest.NewMemStore()}
	rec := get(t, newCatalogAPI(store), "/sitemap.xml")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var doc struct {
		XMLName xml.Name `xml:"urlset"`
		URLs    []struct {
			Loc string `xml:"loc"`
		} `xml:"url"`
	}
	if err := xml.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("invalid XML: %v\n%s", err, rec.Body.String())
	}
	if doc.XMLName.Local != "urlset" || len(doc.URLs) != 0 {
		t.Errorf("empty sitemap = %+v", doc)
	}
}

func TestSitemapMountedAtRoot(t *testing.T) {
	store := &catalogStore{MemStore: tierlisttest.NewMemStore()}
	h := web.NewHandler(web.Deps{API: newCatalogAPI(store)})
	rec := get(t, h, "/sitemap.xml")
	if rec.Code != http.StatusOK {
		t.Fatalf("root sitemap status = %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCatalogAndSitemapWithService(t *testing.T) {
	e := newEnv(t, nil)
	public := e.do(t, http.MethodPost, "/tierlists", "anna", createBody)
	if public.Code != http.StatusCreated {
		t.Fatalf("create public: %d %s", public.Code, public.Body.String())
	}
	publicSlug := decodeMap(t, public)["slug"].(string)

	unlistedBody := strings.Replace(createBody, `"visibility": "public"`, `"visibility": "unlisted"`, 1)
	unlistedBody = strings.Replace(unlistedBody, `"title": "Languages"`, `"title": "Secret poll"`, 1)
	unlisted := e.do(t, http.MethodPost, "/tierlists", "anna", unlistedBody)
	if unlisted.Code != http.StatusCreated {
		t.Fatalf("create unlisted: %d %s", unlisted.Code, unlisted.Body.String())
	}
	unlistedSlug := decodeMap(t, unlisted)["slug"].(string)

	rec := e.do(t, http.MethodGet, "/catalog?sort=popular", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("catalog: %d %s", rec.Code, rec.Body.String())
	}
	body := decodeMap(t, rec)
	items, _ := body["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("catalog items = %v, want only the public poll", items)
	}
	if slug := items[0].(map[string]any)["slug"]; slug != publicSlug {
		t.Errorf("catalog slug = %v, want %s", slug, publicSlug)
	}

	rec = e.do(t, http.MethodGet, "/catalog?q=Secret", "", "")
	if items, _ := decodeMap(t, rec)["items"].([]any); len(items) != 0 {
		t.Errorf("search leaked unlisted: %v", items)
	}

	rec = get(t, e.h, "/sitemap.xml")
	if rec.Code != http.StatusOK {
		t.Fatalf("sitemap: %d %s", rec.Code, rec.Body.String())
	}
	xmlBody := rec.Body.String()
	if !strings.Contains(xmlBody, "/t/"+publicSlug) {
		t.Errorf("sitemap misses public poll:\n%s", xmlBody)
	}
	if strings.Contains(xmlBody, unlistedSlug) {
		t.Errorf("sitemap contains unlisted poll:\n%s", xmlBody)
	}
}
