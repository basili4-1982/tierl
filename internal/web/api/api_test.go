package api_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/basili4-1982/tierl/internal/platform/ratelimit"
	"github.com/basili4-1982/tierl/internal/tierlist"
	"github.com/basili4-1982/tierl/internal/tierlist/tierlisttest"
	"github.com/basili4-1982/tierl/internal/web/api"
)

// headerAuth is a fake Authenticator: the user is the X-Test-User header.
type headerAuth struct{}

func (headerAuth) UserID(r *http.Request) (string, bool) {
	id := r.Header.Get("X-Test-User")
	return id, id != ""
}

type env struct {
	h     http.Handler
	store *tierlisttest.MemStore
}

func newEnv(t *testing.T, limit api.Middleware) env {
	t.Helper()
	store := tierlisttest.NewMemStore()
	users := tierlisttest.Users{
		"anna": {DisplayName: "Anna", EmailVerified: true},
		"bob":  {DisplayName: "Bob", EmailVerified: true},
		"newb": {DisplayName: "Newbie"},
	}
	svc := tierlist.NewService(store, users, tierlist.ServiceDeps{
		Now: func() time.Time { return time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC) },
	})
	h := api.New(api.Deps{
		Tierlists:           svc,
		Auth:                headerAuth{},
		Log:                 slog.New(slog.NewTextHandler(io.Discard, nil)),
		CreateTierlistLimit: limit,
	})
	return env{h: h, store: store}
}

func (e env) do(t *testing.T, method, path, user, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, api.Prefix+path, strings.NewReader(body))
	if user != "" {
		req.Header.Set("X-Test-User", user)
	}
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	return rec
}

const createBody = `{
  "title": "Languages", "description": "Rank them", "visibility": "public", "slug": "langs",
  "tiers": [{"name":"S","color":"#ff7f7f"},{"name":"A","color":"#ffbf7f"}],
  "items": [{"label":"Go"},{"label":"Rust","image_url":"https://cdn.example.com/rust.png"},{"label":"Zig","image_url":null}]
}`

func decodeMap(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("body %q: %v", rec.Body.String(), err)
	}
	return m
}

func wantError(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) map[string]any {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status = %d, want %d; body %s", rec.Code, status, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q", ct)
	}
	m := decodeMap(t, rec)
	if m["code"] != code {
		t.Fatalf("code = %v, want %s; body %s", m["code"], code, rec.Body.String())
	}
	if msg, _ := m["message"].(string); msg == "" {
		t.Error("message is empty")
	}
	return m
}

func TestCreateAndGet(t *testing.T) {
	e := newEnv(t, nil)

	rec := e.do(t, http.MethodPost, "/tierlists", "anna", createBody)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	m := decodeMap(t, rec)
	if m["slug"] != "langs" || m["status"] != "open" || m["visibility"] != "public" || m["is_owner"] != true {
		t.Errorf("unexpected body: %v", m)
	}
	if m["closed_at"] != nil || m["submissions_count"] != float64(0) {
		t.Errorf("closed_at/submissions_count: %v", m)
	}
	if a, _ := m["author"].(map[string]any); a["id"] != "anna" || a["display_name"] != "Anna" {
		t.Errorf("author = %v", m["author"])
	}
	items, _ := m["items"].([]any)
	if len(items) != 3 {
		t.Fatalf("items = %v", items)
	}
	if it := items[1].(map[string]any); it["image_url"] != "https://cdn.example.com/rust.png" || it["position"] != float64(1) {
		t.Errorf("item 1 = %v", it)
	}
	if it := items[0].(map[string]any); it["image_url"] != nil {
		t.Errorf("item 0 image_url = %v, want null", it["image_url"])
	}
	if _, err := time.Parse(time.RFC3339, m["created_at"].(string)); err != nil {
		t.Errorf("created_at: %v", err)
	}

	for user, wantOwner := range map[string]bool{"": false, "bob": false, "anna": true} {
		rec = e.do(t, http.MethodGet, "/tierlists/langs", user, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("get as %q: %d", user, rec.Code)
		}
		if got := decodeMap(t, rec)["is_owner"]; got != wantOwner {
			t.Errorf("is_owner as %q = %v, want %v", user, got, wantOwner)
		}
	}
	wantError(t, e.do(t, http.MethodGet, "/tierlists/missing", "", ""), http.StatusNotFound, "not_found")
}

func TestCreate_Errors(t *testing.T) {
	e := newEnv(t, nil)
	tests := []struct {
		name   string
		user   string
		body   string
		status int
		code   string
	}{
		{"guest", "", createBody, 401, "unauthorized"},
		{"email not verified", "newb", createBody, 403, "email_not_verified"},
		{"malformed json", "anna", `{`, 400, "bad_request"},
		{"unknown field", "anna", `{"title":"x","nope":1}`, 400, "bad_request"},
		{"trailing data", "anna", createBody + `{}`, 400, "bad_request"},
		{"wrong type", "anna", `{"title": 5}`, 400, "bad_request"},
		{"validation", "anna", `{"title":"","visibility":"public","slug":"s1","tiers":[],"items":[]}`, 422, "validation_failed"},
		{"public without slug", "anna", strings.Replace(createBody, `"slug": "langs",`, "", 1), 422, "validation_failed"},
		{"bad visibility", "anna", strings.Replace(createBody, `"public"`, `"secret"`, 1), 422, "validation_failed"},
		{"bad image url", "anna", strings.Replace(createBody, `https://cdn.example.com/rust.png`, `javascript:alert(1)`, 1), 422, "validation_failed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wantError(t, e.do(t, http.MethodPost, "/tierlists", tt.user, tt.body), tt.status, tt.code)
		})
	}
	if e.store.Len() != 0 {
		t.Errorf("failed requests stored %d tierlists", e.store.Len())
	}
}

func TestCreate_ValidationDetails(t *testing.T) {
	e := newEnv(t, nil)
	body := strings.Replace(createBody, `"title": "Languages"`, `"title": ""`, 1)
	m := wantError(t, e.do(t, http.MethodPost, "/tierlists", "anna", body), 422, "validation_failed")
	fields, _ := m["details"].(map[string]any)["fields"].(map[string]any)
	if fields["title"] == nil {
		t.Errorf("details = %v", m["details"])
	}
}

func TestCreate_SlugTaken(t *testing.T) {
	e := newEnv(t, nil)
	if rec := e.do(t, http.MethodPost, "/tierlists", "anna", createBody); rec.Code != http.StatusCreated {
		t.Fatal(rec.Body.String())
	}
	m := wantError(t, e.do(t, http.MethodPost, "/tierlists", "bob", createBody), 409, "slug_taken")
	if m["details"] == nil {
		t.Error("slug_taken must point at the slug field")
	}
}

func TestCreate_Unlisted(t *testing.T) {
	e := newEnv(t, nil)
	body := strings.Replace(createBody, `"public"`, `"unlisted"`, 1)
	rec := e.do(t, http.MethodPost, "/tierlists", "anna", body)
	if rec.Code != http.StatusCreated {
		t.Fatal(rec.Body.String())
	}
	if s := decodeMap(t, rec)["slug"].(string); s == "langs" || len(s) < 12 {
		t.Errorf("slug = %q", s)
	}
}

func TestCreate_InternalError(t *testing.T) {
	e := newEnv(t, nil)
	e.store.Errs["Create"] = errors.New("db exploded: secret detail")
	rec := e.do(t, http.MethodPost, "/tierlists", "anna", createBody)
	m := wantError(t, rec, 500, "internal_error")
	if strings.Contains(rec.Body.String(), "secret detail") {
		t.Error("internal error details leaked to the client")
	}
	_ = m
}

func TestCreate_RateLimit(t *testing.T) {
	lim, err := ratelimit.NewMemory(ratelimit.Config{Burst: 2, Interval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	e := newEnv(t, ratelimit.Middleware(lim, ratelimit.IPKey([]byte("salt"), "create_tierlist")))
	for i, slug := range []string{"one", "two"} {
		body := strings.Replace(createBody, `"langs"`, `"`+slug+`"`, 1)
		if rec := e.do(t, http.MethodPost, "/tierlists", "anna", body); rec.Code != http.StatusCreated {
			t.Fatalf("request %d: %d %s", i, rec.Code, rec.Body.String())
		}
	}
	rec := e.do(t, http.MethodPost, "/tierlists", "anna", createBody)
	wantError(t, rec, http.StatusTooManyRequests, "rate_limited")
	if rec.Header().Get("Retry-After") == "" {
		t.Error("Retry-After is missing")
	}
	// Reads are not limited.
	if rec := e.do(t, http.MethodGet, "/tierlists/one", "", ""); rec.Code != http.StatusOK {
		t.Errorf("GET limited: %d", rec.Code)
	}
}

func TestPatch(t *testing.T) {
	e := newEnv(t, nil)
	e.do(t, http.MethodPost, "/tierlists", "anna", createBody)

	rec := e.do(t, http.MethodPatch, "/tierlists/langs", "anna", `{"title":"New","visibility":"unlisted"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch: %d %s", rec.Code, rec.Body.String())
	}
	m := decodeMap(t, rec)
	if m["title"] != "New" || m["visibility"] != "unlisted" || m["slug"] == "langs" {
		t.Errorf("unexpected body: %v", m)
	}
	slug := m["slug"].(string)

	rec = e.do(t, http.MethodPatch, "/tierlists/"+slug, "anna",
		`{"tiers":[{"name":"X","color":"#000000"},{"name":"Y","color":"#ffffff"},{"name":"Z","color":"#123456"}],"items":null}`)
	if rec.Code != http.StatusOK || len(decodeMap(t, rec)["tiers"].([]any)) != 3 {
		t.Fatalf("structure patch: %d %s", rec.Code, rec.Body.String())
	}
}

func TestPatch_LockedAfterSubmission(t *testing.T) {
	e := newEnv(t, nil)
	rec := e.do(t, http.MethodPost, "/tierlists", "anna", createBody)
	e.store.SetSubmissions(decodeMap(t, rec)["id"].(string), 1)

	structure := `{"items":[{"label":"a"},{"label":"b"},{"label":"c"}]}`
	wantError(t, e.do(t, http.MethodPatch, "/tierlists/langs", "anna", structure), 409, "tierlist_locked")

	if rec := e.do(t, http.MethodPatch, "/tierlists/langs", "anna", `{"title":"Fine","description":"d"}`); rec.Code != http.StatusOK {
		t.Fatalf("text after answers: %d %s", rec.Code, rec.Body.String())
	}
	if got := decodeMap(t, e.do(t, http.MethodGet, "/tierlists/langs", "", ""))["submissions_count"]; got != float64(1) {
		t.Errorf("submissions_count = %v", got)
	}
}

func TestPatch_Errors(t *testing.T) {
	e := newEnv(t, nil)
	e.do(t, http.MethodPost, "/tierlists", "anna", createBody)
	tests := []struct {
		name   string
		user   string
		slug   string
		body   string
		status int
		code   string
	}{
		{"guest", "", "langs", `{"title":"x"}`, 401, "unauthorized"},
		{"not author", "bob", "langs", `{"title":"x"}`, 403, "forbidden"},
		{"missing", "anna", "nothing", `{"title":"x"}`, 404, "not_found"},
		{"empty object", "anna", "langs", `{}`, 400, "bad_request"},
		{"bad json", "anna", "langs", `[`, 400, "bad_request"},
		{"empty title", "anna", "langs", `{"title":""}`, 422, "validation_failed"},
		{"bad visibility", "anna", "langs", `{"visibility":"secret"}`, 422, "validation_failed"},
		{"too few items", "anna", "langs", `{"items":[]}`, 422, "validation_failed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wantError(t, e.do(t, http.MethodPatch, "/tierlists/"+tt.slug, tt.user, tt.body), tt.status, tt.code)
		})
	}
}

func TestClose(t *testing.T) {
	e := newEnv(t, nil)
	e.do(t, http.MethodPost, "/tierlists", "anna", createBody)

	wantError(t, e.do(t, http.MethodPost, "/tierlists/langs/close", "", ""), 401, "unauthorized")
	wantError(t, e.do(t, http.MethodPost, "/tierlists/langs/close", "bob", ""), 403, "forbidden")
	wantError(t, e.do(t, http.MethodPost, "/tierlists/nothing/close", "anna", ""), 404, "not_found")

	for range 2 { // idempotent
		rec := e.do(t, http.MethodPost, "/tierlists/langs/close", "anna", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("close: %d %s", rec.Code, rec.Body.String())
		}
		m := decodeMap(t, rec)
		if m["status"] != "closed" || m["closed_at"] != "2026-10-22T09:00:00Z" {
			t.Errorf("unexpected body: %v", m)
		}
	}
}

func TestDelete(t *testing.T) {
	e := newEnv(t, nil)
	e.do(t, http.MethodPost, "/tierlists", "anna", createBody)

	wantError(t, e.do(t, http.MethodDelete, "/tierlists/langs", "", ""), 401, "unauthorized")
	wantError(t, e.do(t, http.MethodDelete, "/tierlists/langs", "bob", ""), 403, "forbidden")

	rec := e.do(t, http.MethodDelete, "/tierlists/langs", "anna", "")
	if rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
		t.Fatalf("delete: %d %q", rec.Code, rec.Body.String())
	}
	wantError(t, e.do(t, http.MethodGet, "/tierlists/langs", "", ""), 404, "not_found")
	wantError(t, e.do(t, http.MethodDelete, "/tierlists/langs", "anna", ""), 404, "not_found")
}

func TestStatusInvalidTransition(t *testing.T) {
	// A closed list cannot be reopened via close again, but hidden ones are invisible.
	e := newEnv(t, nil)
	rec := e.do(t, http.MethodPost, "/tierlists", "anna", createBody)
	id := decodeMap(t, rec)["id"].(string)
	tl, _ := e.store.GetByID(t.Context(), id)
	tl.Status = "hidden"
	e.store.Put(*tl)
	wantError(t, e.do(t, http.MethodGet, "/tierlists/langs", "anna", ""), 404, "not_found")
	wantError(t, e.do(t, http.MethodPost, "/tierlists/langs/close", "anna", ""), 404, "not_found")
}

func TestUnknownRoute(t *testing.T) {
	e := newEnv(t, nil)
	wantError(t, e.do(t, http.MethodGet, "/nothing", "", ""), 404, "not_found")
}

func TestBodyTooLarge(t *testing.T) {
	e := newEnv(t, nil)
	big := `{"title":"` + strings.Repeat("a", 2<<20) + `"}`
	wantError(t, e.do(t, http.MethodPost, "/tierlists", "anna", big), 400, "bad_request")
}

func TestDefaultLogger(t *testing.T) {
	svc := tierlist.NewService(tierlisttest.NewMemStore(), tierlisttest.Users{}, tierlist.ServiceDeps{})
	h := api.New(api.Deps{Tierlists: svc, Auth: headerAuth{}})
	req := httptest.NewRequest(http.MethodGet, api.Prefix+"/tierlists/abc", bytes.NewReader(nil))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d", rec.Code)
	}
}
