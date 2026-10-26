package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/basili4-1982/tierl/internal/tierlist"
	"github.com/basili4-1982/tierl/internal/web/api"
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
	rec := e.do(t, http.MethodPost, "/tierlists", "anna", createBody)
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
