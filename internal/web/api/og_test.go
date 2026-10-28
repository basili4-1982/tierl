package api_test

import (
	"bytes"
	"html"
	"image/png"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"code.sarnas.ru/tierl/internal/domain"
	"code.sarnas.ru/tierl/internal/tierlist/tierlisttest"
	"code.sarnas.ru/tierl/internal/web/api"
)

const ogTestBase = "http://tierl.example"

type ogFixture struct {
	handler http.Handler
	store   *tierlisttest.MemStore
	public  domain.Tierlist
	private domain.Tierlist
}

func newOGFixture(t *testing.T) ogFixture {
	t.Helper()
	store := tierlisttest.NewMemStore()
	users := tierlisttest.Users{"anna": {DisplayName: "Anna", EmailVerified: true}}
	h := api.New(api.Deps{
		Store: store,
		Users: users,
		Auth:  headerAuth{},
		Log:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	})

	now := time.Date(2026, 10, 28, 10, 0, 0, 0, time.UTC)
	title := `Tier <script>alert("x")</script> & "quotes"`
	description := `Rank <b>them</b> & "share"`

	public := domain.Tierlist{
		ID: "result_public", Slug: "langs", AuthorID: "anna",
		Title: title, Description: description,
		Visibility: domain.VisibilityPublic, Status: domain.StatusOpen,
		CreatedAt: now, UpdatedAt: now,
		Tiers: []domain.Tier{
			{ID: "tier-s", Name: "S", Color: "#ff7f7f", Position: 0},
			{ID: "tier-a", Name: "A", Color: "#ffbf7f", Position: 1},
		},
		Items: []domain.Item{
			{ID: "item-go", Label: "Go", Position: 0},
			{ID: "item-rust", Label: "Rust", Position: 1},
			{ID: "item-zig", Label: "Zig", Position: 2},
		},
	}
	private := domain.Tierlist{
		ID: "result_private", Slug: "private-langs", AuthorID: "anna",
		Title: `Unlisted <script>alert("x")</script>`, Description: `Private <b>list</b> & "share"`,
		Visibility: domain.VisibilityUnlisted, Status: domain.StatusOpen,
		CreatedAt: now, UpdatedAt: now,
		Tiers: public.Tiers, Items: public.Items,
	}
	store.Put(public)
	store.Put(private)
	return ogFixture{handler: h, store: store, public: public, private: private}
}

func ogGet(t *testing.T, h http.Handler, rawURL string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, rawURL, nil))
	return rec
}

func ogMeta(t *testing.T, body, attr, key string) string {
	t.Helper()
	key = regexp.QuoteMeta(key)
	patterns := []string{
		`(?is)<meta[^>]*\b` + attr + `\s*=\s*"` + key + `"[^>]*\bcontent\s*=\s*"([^"]*)"`,
		`(?is)<meta[^>]*\bcontent\s*=\s*"([^"]*)"[^>]*\b` + attr + `\s*=\s*"` + key + `"`,
	}
	for _, pattern := range patterns {
		if m := regexp.MustCompile(pattern).FindStringSubmatch(body); len(m) == 2 {
			return html.UnescapeString(m[1])
		}
	}
	t.Fatalf("meta %s=%q is missing from:\n%s", attr, key, body)
	return ""
}

func assertOGPage(t *testing.T, rec *httptest.ResponseRecorder, wantTitle, wantImage string, unlisted bool) {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}
	body := rec.Body.String()
	if got := ogMeta(t, body, "property", "og:title"); got != wantTitle {
		t.Errorf("og:title = %q, want %q", got, wantTitle)
	}
	if got := ogMeta(t, body, "property", "og:image"); got != wantImage {
		t.Errorf("og:image = %q, want %q", got, wantImage)
	}
	if got := ogMeta(t, body, "name", "twitter:card"); got != "summary_large_image" {
		t.Errorf("twitter:card = %q", got)
	}
	if got := ogMeta(t, body, "name", "twitter:image"); got != wantImage {
		t.Errorf("twitter:image = %q, want %q", got, wantImage)
	}
	if strings.Contains(body, `<script>alert(`) {
		t.Error("user text is not escaped: raw script tag was rendered")
	}
	if unlisted {
		if got := rec.Header().Get("Cache-Control"); got != "private, no-store" {
			t.Errorf("Cache-Control = %q, want private, no-store", got)
		}
		if got := rec.Header().Get("X-Robots-Tag"); got != "noindex, nofollow" {
			t.Errorf("X-Robots-Tag = %q, want noindex, nofollow", got)
		}
		if !strings.Contains(body, `<meta name="robots" content="noindex,nofollow">`) {
			t.Error("unlisted page has no noindex meta tag")
		}
	} else {
		if got := rec.Header().Get("Cache-Control"); got != "public, max-age=300, s-maxage=3600" {
			t.Errorf("Cache-Control = %q, want public, max-age=300, s-maxage=3600", got)
		}
		if got := rec.Header().Get("X-Robots-Tag"); got != "" {
			t.Errorf("X-Robots-Tag = %q, want empty", got)
		}
		if strings.Contains(body, `name="robots"`) {
			t.Error("public page must not have a noindex meta tag")
		}
	}
}

func assertOGImage(t *testing.T, rec *httptest.ResponseRecorder, unlisted bool) string {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "image/png" {
		t.Errorf("Content-Type = %q, want image/png", got)
	}
	if unlisted {
		if got := rec.Header().Get("Cache-Control"); got != "private, no-store" {
			t.Errorf("Cache-Control = %q, want private, no-store", got)
		}
		if got := rec.Header().Get("X-Robots-Tag"); got != "noindex, nofollow" {
			t.Errorf("X-Robots-Tag = %q, want noindex, nofollow", got)
		}
	} else {
		if got := rec.Header().Get("Cache-Control"); got != "public, max-age=86400, s-maxage=604800, immutable" {
			t.Errorf("Cache-Control = %q", got)
		}
		if got := rec.Header().Get("X-Robots-Tag"); got != "" {
			t.Errorf("X-Robots-Tag = %q, want empty", got)
		}
	}
	etag := rec.Header().Get("ETag")
	if etag == "" || !strings.HasPrefix(etag, `"`) || !strings.HasSuffix(etag, `"`) {
		t.Errorf("ETag = %q, want a quoted validator", etag)
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatalf("decode PNG: %v", err)
	}
	if cfg.Width != 1200 || cfg.Height != 630 {
		t.Errorf("PNG size = %dx%d, want 1200x630", cfg.Width, cfg.Height)
	}
	return etag
}

func assertOGPageImage(t *testing.T, f ogFixture, pageURL, wantTitle string, unlisted bool) {
	t.Helper()
	page := ogGet(t, f.handler, pageURL)
	imageURL := ogMeta(t, page.Body.String(), "property", "og:image")
	// og:image points at the server-rendered image route of the same object.
	if !strings.HasPrefix(imageURL, ogTestBase+"/og/") || !strings.HasSuffix(imageURL, ".png") {
		t.Fatalf("og:image = %q, want an absolute /og/...png URL", imageURL)
	}
	assertOGPage(t, page, wantTitle, imageURL, unlisted)
	image := ogGet(t, f.handler, imageURL)
	etag := assertOGImage(t, image, unlisted)

	again := ogGet(t, f.handler, imageURL)
	if got := assertOGImage(t, again, unlisted); got != etag {
		t.Errorf("ETag changed between requests: %q then %q", etag, got)
	}
}

func TestShareTermPageAndImage(t *testing.T) {
	f := newOGFixture(t)
	pageURL := ogTestBase + "/t/" + f.public.Slug
	assertOGPageImage(t, f, pageURL, f.public.Title, false)

	page := ogGet(t, f.handler, pageURL)
	body := page.Body.String()
	if !strings.Contains(body, html.EscapeString(f.public.Title)) {
		t.Errorf("escaped title %q is missing from the page", html.EscapeString(f.public.Title))
	}
	if !strings.Contains(body, html.EscapeString(f.public.Description)) {
		t.Errorf("escaped description %q is missing from the page", html.EscapeString(f.public.Description))
	}
}

func TestShareResultPageAndImage(t *testing.T) {
	f := newOGFixture(t)
	pageURL := ogTestBase + "/r/" + f.public.ID
	assertOGPageImage(t, f, pageURL, f.public.Title+" — результат", false)
}

func TestShareUnlistedTermAndResult(t *testing.T) {
	f := newOGFixture(t)

	t.Run("term", func(t *testing.T) {
		pageURL := ogTestBase + "/t/" + f.private.Slug
		assertOGPageImage(t, f, pageURL, f.private.Title, true)
	})
	t.Run("result", func(t *testing.T) {
		pageURL := ogTestBase + "/r/" + f.private.ID
		assertOGPageImage(t, f, pageURL, f.private.Title+" — результат", true)
	})
}
