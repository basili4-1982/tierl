package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"code.sarnas.ru/tierl/internal/tierlist"
	"code.sarnas.ru/tierl/internal/web/api"
)

func (e env) doCookie(t *testing.T, method, path, user, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, api.Prefix+path, strings.NewReader(body))
	if user != "" {
		req.Header.Set("X-Test-User", user)
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	if method != http.MethodGet && method != http.MethodHead {
		req.Header.Set(api.CSRFHeader, e.csrf)
	}
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	return rec
}

func anonCookie(t *testing.T, rec *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == api.AnonCookieName {
			return c
		}
	}
	t.Fatalf("response has no %s cookie: %v", api.AnonCookieName, rec.Header())
	return nil
}

type listIDs struct {
	slug  string
	items []string
	tiers []string
}

func createListIDs(t *testing.T, e env) listIDs {
	t.Helper()
	return createListIDsWithBody(t, e, createBody)
}

func createListIDsWithBody(t *testing.T, e env, body string) listIDs {
	t.Helper()
	rec := e.do(t, http.MethodPost, "/tierlists", "anna", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	m := decodeMap(t, rec)
	out := listIDs{slug: m["slug"].(string)}
	for _, item := range m["items"].([]any) {
		out.items = append(out.items, item.(map[string]any)["id"].(string))
	}
	for _, tier := range m["tiers"].([]any) {
		out.tiers = append(out.tiers, tier.(map[string]any)["id"].(string))
	}
	return out
}

func submissionBody(ids listIDs, tier int, n int) string {
	placements := make([]map[string]string, 0, n)
	for i := 0; i < n; i++ {
		placements = append(placements, map[string]string{"item_id": ids.items[i], "tier_id": ids.tiers[tier]})
	}
	b, _ := json.Marshal(map[string]any{"placements": placements})
	return string(b)
}

func TestSubmissionEndpoints(t *testing.T) {
	e := newEnv(t, nil)
	ids := createListIDs(t, e)

	rec := e.do(t, http.MethodPost, "/tierlists/"+ids.slug+"/submissions", "", submissionBody(ids, 0, 2))
	wantError(t, rec, http.StatusBadRequest, "bad_request")

	rec = e.do(t, http.MethodPost, "/tierlists/"+ids.slug+"/submissions", "", submissionBody(ids, 0, 3))
	if rec.Code != http.StatusCreated {
		t.Fatalf("submit: %d %s", rec.Code, rec.Body.String())
	}
	cookie := anonCookie(t, rec)
	first := decodeMap(t, rec)
	resultID := first["id"].(string)
	if first["tierlist_slug"] != ids.slug || len(first["placements"].([]any)) != 3 {
		t.Fatalf("submission = %v", first)
	}
	if strings.Contains(rec.Body.String(), "session_id") || strings.Contains(rec.Body.String(), "user_id") {
		t.Fatalf("submission leaks an identifier: %s", rec.Body.String())
	}
	if _, err := time.Parse(time.RFC3339, first["created_at"].(string)); err != nil {
		t.Errorf("created_at: %v", err)
	}

	rec = e.do(t, http.MethodPost, "/tierlists/"+ids.slug+"/submissions", "", submissionBody(ids, 0, 3))
	if rec.Code != http.StatusCreated {
		t.Fatalf("a separate anonymous session must be a separate answer: %d %s", rec.Code, rec.Body.String())
	}

	conflict := e.doCookie(t, http.MethodPost, "/tierlists/"+ids.slug+"/submissions", "", submissionBody(ids, 1, 3), cookie)
	m := wantError(t, conflict, http.StatusConflict, "already_submitted")
	if details, _ := m["details"].(map[string]any); details["result_id"] != resultID {
		t.Fatalf("details = %v", m["details"])
	}

	rec = e.doCookie(t, http.MethodPut, "/tierlists/"+ids.slug+"/submissions/mine", "", submissionBody(ids, 1, 3), cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("replace: %d %s", rec.Code, rec.Body.String())
	}
	replaced := decodeMap(t, rec)
	if replaced["id"] != resultID || replaced["placements"].([]any)[0].(map[string]any)["tier_id"] != ids.tiers[1] {
		t.Fatalf("replaced = %v", replaced)
	}

	rec = e.doCookie(t, http.MethodGet, "/tierlists/"+ids.slug+"/submissions/mine", "", "", cookie)
	if rec.Code != http.StatusOK || decodeMap(t, rec)["id"] != resultID {
		t.Fatalf("mine: %d %s", rec.Code, rec.Body.String())
	}
	other := &http.Cookie{Name: api.AnonCookieName, Value: "another-session"}
	wantError(t, e.doCookie(t, http.MethodGet, "/tierlists/"+ids.slug+"/submissions/mine", "", "", other), http.StatusNotFound, "not_found")

	rec = e.do(t, http.MethodGet, "/results/"+resultID, "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("result: %d %s", rec.Code, rec.Body.String())
	}
	res := decodeMap(t, rec)
	sub := res["submission"].(map[string]any)
	if sub["id"] != resultID || sub["tierlist_slug"] != ids.slug {
		t.Fatalf("result = %v", res)
	}
	if res["tierlist"].(map[string]any)["slug"] != ids.slug {
		t.Fatalf("result tierlist = %v", res["tierlist"])
	}
	if strings.Contains(rec.Body.String(), "session_id") || strings.Contains(rec.Body.String(), "user_id") {
		t.Fatalf("result leaks an identifier: %s", rec.Body.String())
	}
	wantError(t, e.do(t, http.MethodGet, "/results/missing", "", ""), http.StatusNotFound, "not_found")
}

func TestResultStatsBoundariesThroughAPI(t *testing.T) {
	e := newEnv(t, nil)
	ids := createListIDs(t, e)
	tl, err := e.store.GetBySlug(t.Context(), ids.slug)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	placements := func(tier int) []tierlist.Placement {
		out := make([]tierlist.Placement, 0, len(ids.items))
		for _, item := range ids.items {
			out = append(out, tierlist.Placement{ItemID: item, TierID: ids.tiers[tier]})
		}
		return out
	}
	save := func(n int) {
		for i := 0; i < n; i++ {
			_, err := e.store.Save(t.Context(), tierlist.Submission{
				ID: fmt.Sprintf("result-%02d", i), TierlistID: tl.ID,
				SessionID: fmt.Sprintf("session-%02d", i), Placements: placements(0),
			}, now.Add(time.Duration(i)*time.Second))
			if err != nil {
				t.Fatal(err)
			}
		}
	}

	save(19)
	rec := e.do(t, http.MethodGet, "/results/result-00/stats", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("stats: %d %s", rec.Code, rec.Body.String())
	}
	stats := decodeMap(t, rec)
	if stats["insufficient_data"] != true || stats["min_submissions"] != float64(20) || stats["like_you_percent"] != nil {
		t.Fatalf("19 answers stats = %v", stats)
	}
	if len(stats["similar"].([]any)) != 0 || len(stats["dissimilar"].([]any)) != 0 {
		t.Fatalf("similar/dissimilar must be empty: %v", stats)
	}
	items := stats["items"].([]any)
	if len(items) != 3 {
		t.Fatalf("items = %v", items)
	}
	dist := items[0].(map[string]any)["distribution"].([]any)
	if len(dist) != 2 || dist[0].(map[string]any)["count"] != float64(19) || dist[0].(map[string]any)["percent"] != float64(100) {
		t.Fatalf("distribution = %v", dist)
	}

	save(20)
	rec = e.do(t, http.MethodGet, "/results/result-00/stats", "", "")
	stats = decodeMap(t, rec)
	if stats["insufficient_data"] != false || stats["total_submissions"] != float64(20) || stats["like_you_percent"] != float64(100) {
		t.Fatalf("20 answers stats = %v", stats)
	}
	if len(stats["similar"].([]any)) != 5 || len(stats["dissimilar"].([]any)) != 5 {
		t.Fatalf("similar/dissimilar = %v / %v", stats["similar"], stats["dissimilar"])
	}
	for _, raw := range stats["similar"].([]any) {
		if raw.(map[string]any)["result_id"] == "result-00" {
			t.Fatal("similar contains the requested result")
		}
	}
}

func TestPartialSubmissionThroughAPI(t *testing.T) {
	e := newEnv(t, nil)
	body := strings.Replace(createBody,
		`{"label":"Zig","image_url":null}`,
		`{"label":"Zig","image_url":null},{"label":"Kotlin","image_url":null}`, 1)
	ids := createListIDsWithBody(t, e, body)
	if len(ids.items) != 4 {
		t.Fatalf("items = %d", len(ids.items))
	}
	unplaced := ids.items[3]

	rec := e.do(t, http.MethodPost, "/tierlists/"+ids.slug+"/submissions", "", submissionBody(ids, 0, 3))
	if rec.Code != http.StatusCreated {
		t.Fatalf("partial submit: %d %s", rec.Code, rec.Body.String())
	}
	sub := decodeMap(t, rec)
	placements, _ := sub["placements"].([]any)
	if len(placements) != 3 {
		t.Fatalf("placements = %v", sub["placements"])
	}
	for _, raw := range placements {
		if raw.(map[string]any)["item_id"] == unplaced {
			t.Fatalf("unplaced item is in placements: %s", rec.Body.String())
		}
	}
	for _, forbidden := range []string{"session_id", "user_id", "email", "ip_address", "client_ip", "127.0.0.1", "::1"} {
		if strings.Contains(rec.Body.String(), forbidden) {
			t.Fatalf("submission leaks %q: %s", forbidden, rec.Body.String())
		}
	}

	rec = e.do(t, http.MethodGet, "/tierlists/"+ids.slug, "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get tierlist: %d %s", rec.Code, rec.Body.String())
	}
	if got := decodeMap(t, rec)["submissions_count"]; got != float64(1) {
		t.Fatalf("submissions_count = %v", got)
	}

	resultID := sub["id"].(string)
	rec = e.do(t, http.MethodGet, "/results/"+resultID+"/stats", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("stats: %d %s", rec.Code, rec.Body.String())
	}
	stats := decodeMap(t, rec)
	if stats["total_submissions"] != float64(1) {
		t.Fatalf("stats total = %v", stats["total_submissions"])
	}
	want := make(map[string]map[string]int, len(ids.items))
	for i, itemID := range ids.items {
		byTier := make(map[string]int, len(ids.tiers))
		for j, tierID := range ids.tiers {
			if i < 3 && j == 0 {
				byTier[tierID] = 1
			} else {
				byTier[tierID] = 0
			}
		}
		want[itemID] = byTier
	}
	assertTierDistribution(t, stats, ids, 1, want)
	for _, forbidden := range []string{"session_id", "user_id", "email", "ip_address", "client_ip", "127.0.0.1", "::1"} {
		if strings.Contains(rec.Body.String(), forbidden) {
			t.Fatalf("stats leaks %q: %s", forbidden, rec.Body.String())
		}
	}
}
