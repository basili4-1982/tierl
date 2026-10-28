package api_test

import (
	"bytes"
	"context"
	"encoding/json"
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
	"code.sarnas.ru/tierl/internal/tierlist"
	"code.sarnas.ru/tierl/internal/tierlist/tierlisttest"
	"code.sarnas.ru/tierl/internal/web/api"
)

const ogTestBase = "http://tierl.example"

// ogServiceResults adapts the tierlist service to api.ResultProvider. Like
// app.ogResultReader it resolves results by the submission ID returned from
// POST /submissions, not by the tier list ID.
type ogServiceResults struct {
	svc *tierlist.Service
}

func (r ogServiceResults) GetOGResult(ctx context.Context, id string) (*api.OGResult, error) {
	d, err := r.svc.Result(ctx, id, "")
	if err != nil {
		return nil, err
	}
	updated := d.Submission.UpdatedAt
	return &api.OGResult{
		ID:         d.Submission.ID,
		Tierlist:   d.Tierlist,
		AuthorName: d.AuthorName,
		Placements: d.Submission.Placements,
		UpdatedAt:  &updated,
	}, nil
}

type ogFixture struct {
	handler   http.Handler
	store     *tierlisttest.MemStore
	public    domain.Tierlist
	private   domain.Tierlist
	publicID  string
	privateID string
}

func newOGFixture(t *testing.T) ogFixture {
	t.Helper()
	store := tierlisttest.NewMemStore()
	users := tierlisttest.Users{"anna": {DisplayName: "Anna", EmailVerified: true}}

	now := time.Date(2026, 10, 28, 10, 0, 0, 0, time.UTC)
	svc := tierlist.NewService(store, users, tierlist.ServiceDeps{
		Now: func() time.Time { return now },
	})
	h := api.New(api.Deps{
		Tierlists: svc,
		Store:     store,
		Results:   ogServiceResults{svc: svc},
		Users:     users,
		Auth:      headerAuth{},
		Log:       slog.New(slog.NewTextHandler(io.Discard, nil)),
	})

	title := `Tier <script>alert("x")</script> & "quotes"`
	description := `Rank <b>them</b> & "share"`

	public := domain.Tierlist{
		ID: "list_public", Slug: "langs", AuthorID: "anna",
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
		ID: "list_private", Slug: "private-langs", AuthorID: "anna",
		Title: `Unlisted <script>alert("x")</script>`, Description: `Private <b>list</b> & "share"`,
		Visibility: domain.VisibilityUnlisted, Status: domain.StatusOpen,
		CreatedAt: now, UpdatedAt: now,
		Tiers: public.Tiers, Items: public.Items,
	}
	store.Put(public)
	store.Put(private)

	csrf := ogCSRF(t, h)
	publicID := ogSubmit(t, h, csrf, public)
	privateID := ogSubmit(t, h, csrf, private)
	if publicID == public.ID || privateID == private.ID {
		t.Fatalf("submission ID equals tier list ID: %q / %q", publicID, privateID)
	}

	return ogFixture{
		handler: h, store: store,
		public: public, private: private,
		publicID: publicID, privateID: privateID,
	}
}

func ogCSRF(t *testing.T, h http.Handler) string {
	t.Helper()
	rec := ogGet(t, h, ogTestBase+api.Prefix+"/csrf")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /csrf: %d %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Token string `json:"csrf_token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Token == "" {
		t.Fatalf("GET /csrf: %q (%v)", rec.Body.String(), err)
	}
	return body.Token
}

// ogSubmit creates a real anonymous submission through the public API and
// returns its result ID.
func ogSubmit(t *testing.T, h http.Handler, csrf string, tl domain.Tierlist) string {
	t.Helper()
	placements := make([]map[string]string, 0, len(tl.Items))
	for _, item := range tl.Items {
		placements = append(placements, map[string]string{
			"item_id": item.ID,
			"tier_id": tl.Tiers[0].ID,
		})
	}
	payload, err := json.Marshal(map[string]any{"placements": placements})
	if err != nil {
		t.Fatalf("marshal submission: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost,
		ogTestBase+api.Prefix+"/tierlists/"+tl.Slug+"/submissions", bytes.NewReader(payload))
	req.Header.Set(api.CSRFHeader, csrf)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /tierlists/%s/submissions: %d %s", tl.Slug, rec.Code, rec.Body.String())
	}
	var result struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil || result.ID == "" {
		t.Fatalf("submission response %q (%v)", rec.Body.String(), err)
	}
	return result.ID
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

func assertOGImage(t *testing.T, rec *httptest.ResponseRecorder, wantETag string, unlisted bool) string {
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
	if wantETag != "" && etag != wantETag {
		t.Errorf("ETag changed between requests: %q then %q", wantETag, etag)
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

func assertOGPageImage(t *testing.T, f ogFixture, pageURL, wantTitle, wantImagePath string, unlisted bool) {
	t.Helper()
	page := ogGet(t, f.handler, pageURL)
	wantImage := ogTestBase + wantImagePath
	// The image URL comes from the page itself, then the same URL is fetched.
	imageURL := ogMeta(t, page.Body.String(), "property", "og:image")
	if imageURL != wantImage {
		t.Fatalf("og:image = %q, want %q", imageURL, wantImage)
	}
	assertOGPage(t, page, wantTitle, imageURL, unlisted)
	image := ogGet(t, f.handler, imageURL)
	etag := assertOGImage(t, image, "", unlisted)

	again := ogGet(t, f.handler, imageURL)
	assertOGImage(t, again, etag, unlisted)
}

func TestShareTermPageAndImage(t *testing.T) {
	f := newOGFixture(t)
	pageURL := ogTestBase + "/t/" + f.public.Slug
	assertOGPageImage(t, f, pageURL, f.public.Title, "/og/t/"+f.public.Slug+".png", false)

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
	if f.publicID == "" {
		t.Fatal("fixture did not create a submission")
	}
	pageURL := ogTestBase + "/r/" + f.publicID
	assertOGPageImage(t, f, pageURL, f.public.Title+" — результат",
		"/og/r/"+f.publicID+".png", false)
}

func TestShareUnlistedTermAndResult(t *testing.T) {
	f := newOGFixture(t)

	t.Run("term", func(t *testing.T) {
		pageURL := ogTestBase + "/t/" + f.private.Slug
		assertOGPageImage(t, f, pageURL, f.private.Title, "/og/t/"+f.private.Slug+".png", true)
	})
	t.Run("result", func(t *testing.T) {
		if f.privateID == "" {
			t.Fatal("fixture did not create a submission")
		}
		pageURL := ogTestBase + "/r/" + f.privateID
		assertOGPageImage(t, f, pageURL, f.private.Title+" — результат",
			"/og/r/"+f.privateID+".png", true)
	})
}
