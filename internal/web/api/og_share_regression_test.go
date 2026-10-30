package api_test

import (
	"context"
	"html"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"code.sarnas.ru/tierl/internal/domain"
	"code.sarnas.ru/tierl/internal/tierlist"
	"code.sarnas.ru/tierl/internal/tierlist/tierlisttest"
	"code.sarnas.ru/tierl/internal/web/api"
)

const (
	regOGPublicSlug       = "reg-public-list"
	regOGUnlistedSlug     = "reg-unlisted-secret"
	regOGPublicResultID   = "reg-public-result"
	regOGUnlistedResultID = "reg-unlisted-result"
)

type regOGResults map[string]*api.OGResult

func (r regOGResults) GetOGResult(_ context.Context, id string) (*api.OGResult, error) {
	res, ok := r[id]
	if !ok {
		return nil, tierlist.ErrNotFound
	}
	return res, nil
}

func regOGFixture(t *testing.T) (http.Handler, *domain.Tierlist, *domain.Tierlist) {
	t.Helper()
	updated := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	tiers := []domain.Tier{
		{ID: "tier-s", Name: "S", Color: "#ff0000", Position: 0},
		{ID: "tier-a", Name: "A", Color: "#00ff00", Position: 1},
	}
	items := []domain.Item{
		{ID: "item-go", Label: "Go", Position: 0},
		{ID: "item-rust", Label: "Rust", Position: 1},
		{ID: "item-zig", Label: "Zig", Position: 2},
	}
	public := &domain.Tierlist{
		ID: "reg-list-public", Slug: regOGPublicSlug, AuthorID: "anna",
		Title:       `Languages <script>alert("x")</script> & "more"`,
		Description: `Rank <b>them</b> & "share"`,
		Visibility:  domain.VisibilityPublic, Status: domain.StatusOpen,
		Items: items, Tiers: tiers, UpdatedAt: updated,
	}
	unlisted := &domain.Tierlist{
		ID: "reg-list-unlisted", Slug: regOGUnlistedSlug, AuthorID: "anna",
		Title:       `Secret <script>alert("secret")</script>`,
		Description: "Only by link",
		Visibility:  domain.VisibilityUnlisted, Status: domain.StatusOpen,
		Items: items, Tiers: tiers, UpdatedAt: updated,
	}
	store := tierlisttest.NewMemStore()
	store.Put(*public)
	store.Put(*unlisted)

	results := regOGResults{
		regOGPublicResultID: {
			ID: regOGPublicResultID, Tierlist: public, AuthorName: "Anna",
			Placements: []tierlist.Placement{
				{ItemID: "item-go", TierID: "tier-s"},
				{ItemID: "item-rust", TierID: "tier-a"},
				{ItemID: "item-zig", TierID: "tier-a"},
			},
			UpdatedAt: &updated,
		},
		regOGUnlistedResultID: {
			ID: regOGUnlistedResultID, Tierlist: unlisted, AuthorName: "Anon",
			Placements: []tierlist.Placement{
				{ItemID: "item-go", TierID: "tier-a"},
				{ItemID: "item-rust", TierID: "tier-a"},
				{ItemID: "item-zig", TierID: "tier-s"},
			},
			UpdatedAt: &updated,
		},
	}
	h := api.New(api.Deps{
		Store:   store,
		Results: results,
		Users: tierlisttest.Users{
			"anna": {DisplayName: "Anna", EmailVerified: true},
		},
	})
	return h, public, unlisted
}

func regOGGet(t *testing.T, h http.Handler, target string, host string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.Host = host
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func regOGMeta(t *testing.T, body, attr, key string) string {
	t.Helper()
	re := regexp.MustCompile(`(?is)<meta\s+` + regexp.QuoteMeta(attr) + `="` + regexp.QuoteMeta(key) + `"\s+content="([^"]*)"`)
	m := re.FindStringSubmatch(body)
	if len(m) != 2 {
		t.Fatalf("meta %s=%q is missing in:\n%s", attr, key, body)
	}
	return m[1]
}

func regOGImageTarget(t *testing.T, h http.Handler, page string) (string, string) {
	t.Helper()
	rec := regOGGet(t, h, page, "tierl.example")
	if rec.Code != http.StatusOK {
		t.Fatalf("page %s status = %d, want 200", page, rec.Code)
	}
	raw := regOGMeta(t, rec.Body.String(), "property", "og:image")
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse og:image %q: %v", raw, err)
	}
	if u.Host != "tierl.example" {
		t.Errorf("og:image host = %q, want tierl.example", u.Host)
	}
	return raw, u.RequestURI()
}

func TestOGSharePNGFollowsImageURL(t *testing.T) {
	h, public, unlisted := regOGFixture(t)
	tests := []struct {
		name     string
		page     string
		unlisted bool
	}{
		{name: "term public", page: "/t/" + public.Slug},
		{name: "result public", page: "/r/" + regOGPublicResultID},
		{name: "term unlisted", page: "/t/" + unlisted.Slug, unlisted: true},
		{name: "result unlisted", page: "/r/" + regOGUnlistedResultID, unlisted: true},
	}
	etagRe := regexp.MustCompile(`^"[0-9a-f]{16}"$`)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw, target := regOGImageTarget(t, h, tt.page)
			if !strings.HasPrefix(raw, "http://tierl.example/") {
				t.Errorf("og:image = %q, want absolute URL", raw)
			}

			rec := regOGGet(t, h, target, "tierl.example")
			if rec.Code != http.StatusOK {
				t.Fatalf("image %s status = %d, want 200; body %s", target, rec.Code, rec.Body.String())
			}
			if ct := rec.Header().Get("Content-Type"); ct != "image/png" {
				t.Errorf("Content-Type = %q, want image/png", ct)
			}
			img, err := png.Decode(rec.Body)
			if err != nil {
				t.Fatalf("decode PNG from %s: %v", target, err)
			}
			if b := img.Bounds(); b.Dx() != 1200 || b.Dy() != 630 {
				t.Errorf("image bounds = %dx%d, want 1200x630", b.Dx(), b.Dy())
			}
			if etag := rec.Header().Get("ETag"); !etagRe.MatchString(etag) {
				t.Errorf("ETag = %q, want a quoted hex validator", etag)
			}

			cache := rec.Header().Get("Cache-Control")
			robots := rec.Header().Get("X-Robots-Tag")
			if tt.unlisted {
				if !strings.Contains(robots, "noindex") {
					t.Errorf("X-Robots-Tag = %q, want noindex", robots)
				}
				if !strings.Contains(cache, "private") || !strings.Contains(cache, "no-store") || strings.Contains(cache, "public") {
					t.Errorf("Cache-Control = %q, want private, no-store", cache)
				}
				return
			}
			if robots != "" {
				t.Errorf("X-Robots-Tag = %q, want empty for public PNG", robots)
			}
			for _, want := range []string{"public", "max-age", "s-maxage"} {
				if !strings.Contains(cache, want) {
					t.Errorf("Cache-Control = %q, want it to contain %q", cache, want)
				}
			}
		})
	}
}

func TestOGSharePNGFromImageURLEtagIsStable(t *testing.T) {
	h, public, _ := regOGFixture(t)
	for _, page := range []string{"/t/" + public.Slug, "/r/" + regOGPublicResultID} {
		t.Run(page, func(t *testing.T) {
			_, target := regOGImageTarget(t, h, page)
			first := regOGGet(t, h, target, "tierl.example").Header().Get("ETag")
			second := regOGGet(t, h, target, "tierl.example").Header().Get("ETag")
			if first == "" || first != second {
				t.Fatalf("ETag = %q then %q, want a stable non-empty value", first, second)
			}
		})
	}
}

func TestOGSharePagesEscapeAllMeta(t *testing.T) {
	h, public, _ := regOGFixture(t)
	const evilHost = `evil.example"><script>alert(1)</script>`
	pages := []struct {
		name  string
		page  string
		title string
	}{
		{name: "term", page: "/t/" + public.Slug, title: public.Title},
		{name: "result", page: "/r/" + regOGPublicResultID, title: public.Title + " — результат"},
	}
	for _, tt := range pages {
		t.Run(tt.name, func(t *testing.T) {
			rec := regOGGet(t, h, tt.page, "tierl.example")
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			req := httptest.NewRequest(http.MethodGet, tt.page, nil)
			req.Host = "tierl.example"
			req.Header.Set("X-Forwarded-Host", evilHost)
			evil := httptest.NewRecorder()
			h.ServeHTTP(evil, req)
			if evil.Code != http.StatusOK {
				t.Fatalf("status with evil host = %d, want 200", evil.Code)
			}
			body := evil.Body.String()

			if got, want := regOGMeta(t, body, "property", "og:title"), html.EscapeString(tt.title); got != want {
				t.Errorf("og:title = %q, want %q", got, want)
			}
			if got := regOGMeta(t, body, "name", "twitter:card"); got != "summary_large_image" {
				t.Errorf("twitter:card = %q, want summary_large_image", got)
			}
			for _, meta := range []struct{ attr, key string }{
				{"property", "og:image"},
				{"name", "twitter:image"},
			} {
				got := regOGMeta(t, body, meta.attr, meta.key)
				if !strings.Contains(got, html.EscapeString(evilHost)) {
					t.Errorf("%s = %q, want escaped evil host", meta.key, got)
				}
				if strings.Contains(got, `"><script>`) {
					t.Errorf("%s is not escaped: %q", meta.key, got)
				}
			}
			if strings.Contains(body, `"><script>`) {
				t.Error("page contains an unescaped script injection")
			}
		})
	}
}
