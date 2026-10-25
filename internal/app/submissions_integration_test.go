package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/basili4-1982/tierl/internal/anonsession"
	"github.com/basili4-1982/tierl/internal/storage/postgres"
	"github.com/basili4-1982/tierl/internal/tierlist/tierlisttest"
	"github.com/basili4-1982/tierl/internal/web"
)

// testClient keeps the two cookies of one browser-like participant and always
// sends a CSRF token on unsafe requests.
type testClient struct {
	t    *testing.T
	h    http.Handler
	user string
	anon string
	csrf string
}

func newClient(t *testing.T, h http.Handler, user string) *testClient {
	return &testClient{t: t, h: h, user: user}
}

func (c *testClient) do(method, path, body string) *httptest.ResponseRecorder {
	c.t.Helper()
	unsafe := method != http.MethodGet && method != http.MethodHead
	if unsafe && c.csrf == "" {
		c.getCSRF()
	}
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.RemoteAddr = "203.0.113.7:4000"
	if c.user != "" {
		req.Header.Set("X-Test-User", c.user)
	}
	if c.anon != "" {
		req.AddCookie(&http.Cookie{Name: "tierl_anon", Value: c.anon})
	}
	if c.csrf != "" {
		req.AddCookie(&http.Cookie{Name: "tierl_csrf", Value: c.csrf})
		req.Header.Set("X-CSRF-Token", c.csrf)
	}
	rec := httptest.NewRecorder()
	c.h.ServeHTTP(rec, req)
	for _, ck := range rec.Result().Cookies() {
		switch ck.Name {
		case "tierl_anon":
			c.anon = ck.Value
		case "tierl_csrf":
			c.csrf = ck.Value
		}
	}
	return rec
}

func (c *testClient) getCSRF() {
	c.t.Helper()
	rec := httptest.NewRecorder()
	c.h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/csrf", nil))
	if rec.Code != http.StatusOK {
		c.t.Fatalf("csrf: %d %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Token string `json:"csrf_token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out.Token == "" {
		c.t.Fatalf("csrf body: %v %s", err, rec.Body.String())
	}
	c.csrf = out.Token
}

func decodeBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("body %q: %v", rec.Body.String(), err)
	}
	return m
}

func (c *testClient) createTierlist(slug, visibility string) map[string]any {
	c.t.Helper()
	body := fmt.Sprintf(`{"title":"Languages","description":"d","visibility":%q,"slug":%q,
		"tiers":[{"name":"S","color":"#ff0000"},{"name":"A","color":"#00ff00"}],
		"items":[{"label":"Go"},{"label":"Rust"},{"label":"Zig"}]}`, visibility, slug)
	rec := c.do(http.MethodPost, "/api/v1/tierlists", body)
	if rec.Code != http.StatusCreated {
		c.t.Fatalf("create %s: %d %s", slug, rec.Code, rec.Body.String())
	}
	return decodeBody(c.t, rec)
}

func idsOf(m map[string]any, key, idKey string) []string {
	arr, _ := m[key].([]any)
	out := make([]string, 0, len(arr))
	for _, v := range arr {
		row, _ := v.(map[string]any)
		out = append(out, row[idKey].(string))
	}
	return out
}

func placementBody(items, tiers []string) string {
	type p struct {
		ItemID string `json:"item_id"`
		TierID string `json:"tier_id"`
	}
	placements := make([]p, 0, len(items))
	for i, item := range items {
		placements = append(placements, p{ItemID: item, TierID: tiers[i%len(tiers)]})
	}
	b, _ := json.Marshal(map[string]any{"placements": placements})
	return string(b)
}

func newTestDeps(t *testing.T) (http.Handler, *pgxpool.Pool) {
	t.Helper()
	url := testDB(t)
	pool, err := postgres.Open(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	cfg := testConfig(url)
	cfg.CreateTierlistBurst = 100
	users := tierlisttest.Users{
		"anna": {DisplayName: "Anna", EmailVerified: true},
		"bob":  {DisplayName: "Bob", EmailVerified: true},
	}
	deps, err := newDeps(pool, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), headerAuth{}, users)
	if err != nil {
		t.Fatal(err)
	}
	return web.NewHandler(deps), pool
}

func setStatus(t *testing.T, pool *pgxpool.Pool, slug, status string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `UPDATE tierlists SET status = $2 WHERE slug = $1`, slug, status); err != nil {
		t.Fatal(err)
	}
}

func TestSubmissionFlowOnRealDatabase(t *testing.T) {
	h, pool := newTestDeps(t)
	anna := newClient(t, h, "anna")
	created := anna.createTierlist("langs", "public")
	items, tiers := idsOf(created, "items", "id"), idsOf(created, "tiers", "id")

	guest := newClient(t, h, "")
	tooShort := guest.do(http.MethodPost, "/api/v1/tierlists/langs/submissions",
		placementBody(items[:2], tiers))
	if tooShort.Code != http.StatusBadRequest || decodeBody(t, tooShort)["code"] != "too_few_placements" {
		t.Fatalf("too few: %d %s", tooShort.Code, tooShort.Body.String())
	}
	if guest.anon != "" {
		t.Error("a rejected answer must not issue the anon cookie")
	}

	// A request that carries the CSRF cookie but not the matching header is
	// rejected before the answer is stored.
	csrfReq := httptest.NewRequest(http.MethodPost, "/api/v1/tierlists/langs/submissions",
		strings.NewReader(placementBody(items, tiers)))
	csrfReq.AddCookie(&http.Cookie{Name: "tierl_csrf", Value: guest.csrf})
	csrfReq.Header.Set("X-CSRF-Token", "wrong")
	csrfRec := httptest.NewRecorder()
	h.ServeHTTP(csrfRec, csrfReq)
	if csrfRec.Code != http.StatusForbidden || decodeBody(t, csrfRec)["code"] != "csrf_failed" {
		t.Fatalf("csrf mismatch: %d %s", csrfRec.Code, csrfRec.Body.String())
	}

	first := guest.do(http.MethodPost, "/api/v1/tierlists/langs/submissions",
		placementBody(items, tiers))
	if first.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", first.Code, first.Body.String())
	}
	body := decodeBody(t, first)
	resultID := body["id"].(string)
	if guest.anon == "" {
		t.Fatal("the anon cookie is missing")
	}
	c := first.Result().Cookies()
	var anonCookie *http.Cookie
	for _, ck := range c {
		if ck.Name == "tierl_anon" {
			anonCookie = ck
		}
	}
	if anonCookie == nil || !anonCookie.HttpOnly || !anonCookie.Secure || anonCookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("anon cookie attributes: %+v", anonCookie)
	}
	for _, forbidden := range []string{"session_id", "user_id", "email", "ip"} {
		if _, ok := body[forbidden]; ok {
			t.Errorf("public submission leaks %q", forbidden)
		}
	}
	// Only the SHA-256 of the cookie token reaches Postgres.
	var storedSession string
	if err := pool.QueryRow(context.Background(),
		`SELECT session_id FROM submissions WHERE id = $1`, resultID).Scan(&storedSession); err != nil {
		t.Fatal(err)
	}
	if storedSession == guest.anon || storedSession != anonsession.HashToken(guest.anon) {
		t.Errorf("session_id must be the hash, got %q", storedSession)
	}

	mine := guest.do(http.MethodGet, "/api/v1/tierlists/langs/submissions/mine", "")
	if mine.Code != http.StatusOK || decodeBody(t, mine)["id"] != resultID {
		t.Fatalf("mine: %d %s", mine.Code, mine.Body.String())
	}
	list := guest.do(http.MethodGet, "/api/v1/submissions/mine", "")
	if list.Code != http.StatusOK || len(decodeBody(t, list)["items"].([]any)) != 1 {
		t.Fatalf("list mine: %d %s", list.Code, list.Body.String())
	}
	// The anon cookie is not an authentication credential: /me stays closed.
	if rec := guest.do(http.MethodGet, "/api/v1/me/submissions", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("guest /me with anon cookie: %d %s", rec.Code, rec.Body.String())
	}
	if rec := anna.do(http.MethodGet, "/api/v1/me/submissions", ""); rec.Code != http.StatusOK {
		t.Fatalf("author /me: %d %s", rec.Code, rec.Body.String())
	}

	again := guest.do(http.MethodPost, "/api/v1/tierlists/langs/submissions", placementBody(items, tiers))
	if again.Code != http.StatusConflict || decodeBody(t, again)["code"] != "already_submitted" {
		t.Fatalf("repeat: %d %s", again.Code, again.Body.String())
	}
	details, _ := decodeBody(t, again)["details"].(map[string]any)
	if details["result_id"] != resultID {
		t.Errorf("result_id = %v, want %s", details["result_id"], resultID)
	}

	replaced := guest.do(http.MethodPut, "/api/v1/tierlists/langs/submissions/mine",
		placementBody(items, []string{tiers[1]}))
	if replaced.Code != http.StatusOK || decodeBody(t, replaced)["id"] != resultID {
		t.Fatalf("replace: %d %s", replaced.Code, replaced.Body.String())
	}
	mine = guest.do(http.MethodGet, "/api/v1/tierlists/langs/submissions/mine", "")
	if got := decodeBody(t, mine)["placements"].([]any)[0].(map[string]any)["tier_id"]; got != tiers[1] {
		t.Errorf("placement was not replaced: %v", got)
	}

	result := guest.do(http.MethodGet, "/api/v1/results/"+resultID, "")
	if result.Code != http.StatusOK {
		t.Fatalf("result: %d %s", result.Code, result.Body.String())
	}
	rm := decodeBody(t, result)
	if rm["submission"].(map[string]any)["id"] != resultID {
		t.Errorf("result submission: %v", rm["submission"])
	}
	tl := rm["tierlist"].(map[string]any)
	if tl["is_owner"] != false || tl["slug"] != "langs" {
		t.Errorf("guest result tierlist: %v", tl)
	}
	if _, ok := tl["email"]; ok {
		t.Error("result leaks the author email")
	}
	// The anonymous cookie is not an authentication credential.
	if tl["is_owner"] == true {
		t.Error("anon cookie must not make a guest the owner")
	}
	if owner := anna.do(http.MethodGet, "/api/v1/results/"+resultID, ""); decodeBody(t, owner)["tierlist"].(map[string]any)["is_owner"] != true {
		t.Error("the author must see is_owner=true")
	}

	statsRec := guest.do(http.MethodGet, "/api/v1/results/"+resultID+"/stats", "")
	if statsRec.Code != http.StatusOK {
		t.Fatalf("stats: %d %s", statsRec.Code, statsRec.Body.String())
	}
	stats := decodeBody(t, statsRec)
	if stats["total_submissions"] != float64(1) || stats["insufficient_data"] != true ||
		stats["like_you_percent"] != nil || stats["min_submissions"] != float64(20) ||
		stats["similarity_threshold"] != 0.8 {
		t.Fatalf("stats meta: %v", stats)
	}
	if n := len(stats["items"].([]any)); n != 3 {
		t.Errorf("items: %d", n)
	}
	firstItem := stats["items"].([]any)[0].(map[string]any)
	if n := len(firstItem["distribution"].([]any)); n != 2 {
		t.Errorf("distribution: %d", n)
	}
	if len(stats["similar"].([]any)) != 0 || len(stats["dissimilar"].([]any)) != 0 {
		t.Errorf("insufficient data must not rank answers: %v", stats)
	}
}

func TestIncompleteAnswerOnRealDatabase(t *testing.T) {
	h, _ := newTestDeps(t)
	anna := newClient(t, h, "anna")
	body := `{"title":"Four","description":"d","visibility":"public","slug":"four-list",
		"tiers":[{"name":"S","color":"#ff0000"},{"name":"A","color":"#00ff00"}],
		"items":[{"label":"Go"},{"label":"Rust"},{"label":"Zig"},{"label":"C"}]}`
	rec := anna.do(http.MethodPost, "/api/v1/tierlists", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	created := decodeBody(t, rec)
	items, tiers := idsOf(created, "items", "id"), idsOf(created, "tiers", "id")

	// Three of four items: the fourth is skipped and does not take part in
	// similarity.
	guest := newClient(t, h, "")
	answer := guest.do(http.MethodPost, "/api/v1/tierlists/four-list/submissions", placementBody(items[:3], tiers))
	if answer.Code != http.StatusCreated {
		t.Fatalf("incomplete answer: %d %s", answer.Code, answer.Body.String())
	}
	resultID := decodeBody(t, answer)["id"].(string)
	if n := len(decodeBody(t, answer)["placements"].([]any)); n != 3 {
		t.Errorf("placements = %d", n)
	}
	stats := decodeBody(t, newClient(t, h, "").do(http.MethodGet, "/api/v1/results/"+resultID+"/stats", ""))
	rows := stats["items"].([]any)
	if len(rows) != 4 {
		t.Fatalf("items = %d", len(rows))
	}
	last := rows[3].(map[string]any)["distribution"].([]any)
	if last[0].(map[string]any)["count"] != float64(0) {
		t.Errorf("skipped item must have no placements: %v", last)
	}
}

func TestSubmissionAccessRulesOnRealDatabase(t *testing.T) {
	h, pool := newTestDeps(t)
	anna := newClient(t, h, "anna")

	unlisted := anna.createTierlist("ignored", "unlisted")
	slug := unlisted["slug"].(string)
	items, tiers := idsOf(unlisted, "items", "id"), idsOf(unlisted, "tiers", "id")
	guest := newClient(t, h, "")
	if rec := guest.do(http.MethodPost, "/api/v1/tierlists/"+slug+"/submissions", placementBody(items, tiers)); rec.Code != http.StatusCreated {
		t.Fatalf("unlisted: %d %s", rec.Code, rec.Body.String())
	}
	if rec := newClient(t, h, "").do(http.MethodGet, "/api/v1/tierlists/"+slug, ""); rec.Code != http.StatusOK {
		t.Fatalf("unlisted link: %d", rec.Code)
	}

	closed := anna.createTierlist("closed-list", "public")
	ci, ct := idsOf(closed, "items", "id"), idsOf(closed, "tiers", "id")
	if rec := anna.do(http.MethodPost, "/api/v1/tierlists/closed-list/close", ""); rec.Code != http.StatusOK {
		t.Fatalf("close: %d %s", rec.Code, rec.Body.String())
	}
	late := newClient(t, h, "")
	if rec := late.do(http.MethodPost, "/api/v1/tierlists/closed-list/submissions", placementBody(ci, ct)); rec.Code != http.StatusConflict ||
		decodeBody(t, rec)["code"] != "tierlist_closed" {
		t.Fatalf("closed: %d %s", rec.Code, rec.Body.String())
	}

	hidden := anna.createTierlist("hidden-list", "public")
	hi, ht := idsOf(hidden, "items", "id"), idsOf(hidden, "tiers", "id")
	hiddenGuest := newClient(t, h, "")
	hiddenResult := hiddenGuest.do(http.MethodPost, "/api/v1/tierlists/hidden-list/submissions", placementBody(hi, ht))
	if hiddenResult.Code != http.StatusCreated {
		t.Fatalf("seed hidden: %d", hiddenResult.Code)
	}
	resultID := decodeBody(t, hiddenResult)["id"].(string)
	setStatus(t, pool, "hidden-list", "hidden")
	if rec := newClient(t, h, "").do(http.MethodPost, "/api/v1/tierlists/hidden-list/submissions", placementBody(hi, ht)); rec.Code != http.StatusNotFound {
		t.Fatalf("hidden submit: %d %s", rec.Code, rec.Body.String())
	}
	if rec := hiddenGuest.do(http.MethodGet, "/api/v1/results/"+resultID, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("hidden result: %d", rec.Code)
	}
	if rec := hiddenGuest.do(http.MethodGet, "/api/v1/results/"+resultID+"/stats", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("hidden stats: %d", rec.Code)
	}

	draft := anna.createTierlist("draft-list", "public")
	di, dt := idsOf(draft, "items", "id"), idsOf(draft, "tiers", "id")
	setStatus(t, pool, "draft-list", "draft")
	if rec := newClient(t, h, "").do(http.MethodPost, "/api/v1/tierlists/draft-list/submissions", placementBody(di, dt)); rec.Code != http.StatusNotFound {
		t.Fatalf("draft submit: %d %s", rec.Code, rec.Body.String())
	}

	valid := anna.createTierlist("valid-list", "public")
	vi, vt := idsOf(valid, "items", "id"), idsOf(valid, "tiers", "id")
	bad := fmt.Sprintf(`{"placements":[{"item_id":"ghost","tier_id":%q},{"item_id":%q,"tier_id":%q},{"item_id":%q,"tier_id":%q}]}`,
		vt[0], vi[0], vt[0], vi[1], vt[0])
	if rec := newClient(t, h, "").do(http.MethodPost, "/api/v1/tierlists/valid-list/submissions", bad); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("foreign item: %d %s", rec.Code, rec.Body.String())
	}
	duplicate := fmt.Sprintf(`{"placements":[{"item_id":%q,"tier_id":%q},{"item_id":%q,"tier_id":%q},{"item_id":%q,"tier_id":%q}]}`,
		vi[0], vt[0], vi[0], vt[0], vi[1], vt[0])
	if rec := newClient(t, h, "").do(http.MethodPost, "/api/v1/tierlists/valid-list/submissions", duplicate); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("duplicate item: %d %s", rec.Code, rec.Body.String())
	}
}

func TestStatsThresholdAndTopNOnRealDatabase(t *testing.T) {
	h, _ := newTestDeps(t)
	anna := newClient(t, h, "anna")
	created := anna.createTierlist("stats-list", "public")
	items, tiers := idsOf(created, "items", "id"), idsOf(created, "tiers", "id")

	submit := func(session, tier string) string {
		t.Helper()
		c := newClient(t, h, "")
		c.anon = session
		rec := c.do(http.MethodPost, "/api/v1/tierlists/stats-list/submissions", placementBody(items, []string{tier}))
		if rec.Code != http.StatusCreated {
			t.Fatalf("submit %s: %d %s", session, rec.Code, rec.Body.String())
		}
		return decodeBody(t, rec)["id"].(string)
	}
	session := func() string {
		s, err := anonsession.New(nil)
		if err != nil {
			t.Fatal(err)
		}
		return s.Token
	}

	// 20 answers: the target and 10 identical ones agree; 9 opposite answers
	// are at similarity 0 with the target under T=2.
	target := submit(session(), tiers[0])
	for range 10 {
		submit(session(), tiers[0])
	}
	for range 9 {
		submit(session(), tiers[1])
	}

	rec := newClient(t, h, "").do(http.MethodGet, "/api/v1/results/"+target+"/stats", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("stats: %d %s", rec.Code, rec.Body.String())
	}
	stats := decodeBody(t, rec)
	if stats["total_submissions"] != float64(20) || stats["insufficient_data"] != false {
		t.Fatalf("meta: %v", stats)
	}
	if stats["like_you_percent"] != 52.6 {
		t.Errorf("percent = %v, want 52.6", stats["like_you_percent"])
	}
	if stats["similarity_threshold"] != 0.8 || stats["min_submissions"] != float64(20) {
		t.Errorf("threshold/min = %v / %v", stats["similarity_threshold"], stats["min_submissions"])
	}
	similar := stats["similar"].([]any)
	dissimilar := stats["dissimilar"].([]any)
	if len(similar) != 5 || len(dissimilar) != 5 {
		t.Fatalf("top-N: %d / %d", len(similar), len(dissimilar))
	}
	if similar[0].(map[string]any)["similarity"] != float64(1) ||
		dissimilar[0].(map[string]any)["similarity"] != float64(0) {
		t.Errorf("ranks: %v / %v", similar[0], dissimilar[0])
	}
	for _, row := range append(similar, dissimilar...) {
		if _, ok := row.(map[string]any)["result_id"]; ok {
			t.Error("stats must not expose result identifiers")
		}
	}
	// 11 answers chose S and 9 chose A for every item: 55% / 45%.
	counts := stats["items"].([]any)[0].(map[string]any)["distribution"].([]any)
	if counts[0].(map[string]any)["count"] != float64(11) || counts[0].(map[string]any)["percent"] != 55.0 {
		t.Errorf("first tier: %v", counts[0])
	}
	if counts[1].(map[string]any)["percent"] != 45.0 {
		t.Errorf("second tier: %v", counts[1])
	}
}

func TestConcurrentSubmissionsOnRealDatabase(t *testing.T) {
	h, _ := newTestDeps(t)
	anna := newClient(t, h, "anna")
	created := anna.createTierlist("race-list", "public")
	items, tiers := idsOf(created, "items", "id"), idsOf(created, "tiers", "id")
	body := placementBody(items, tiers)

	// All clients share one anon token but get their own CSRF token.
	sess, err := anonsession.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	token := sess.Token

	codes := make(chan int, 8)
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c := newClient(t, h, "")
			c.anon = token
			rec := c.do(http.MethodPost, "/api/v1/tierlists/race-list/submissions", body)
			codes <- rec.Code
		}()
	}
	wg.Wait()
	close(codes)
	createdCount, conflictCount := 0, 0
	for code := range codes {
		switch code {
		case http.StatusCreated:
			createdCount++
		case http.StatusConflict:
			conflictCount++
		default:
			t.Fatalf("unexpected status %d", code)
		}
	}
	if createdCount != 1 || conflictCount != 7 {
		t.Fatalf("created=%d conflict=%d", createdCount, conflictCount)
	}
}
