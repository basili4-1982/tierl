package api_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func submissionWithPlacements(placements ...map[string]string) string {
	b, _ := json.Marshal(map[string]any{"placements": placements})
	return string(b)
}

func placement(itemID, tierID string) map[string]string {
	return map[string]string{"item_id": itemID, "tier_id": tierID}
}

func TestSubmissionRejectsInvalidPlacements(t *testing.T) {
	e := newEnv(t, nil)
	ids := createListIDs(t, e)
	path := "/tierlists/" + ids.slug + "/submissions"

	tests := []struct {
		name string
		body string
	}{
		{
			name: "duplicate item",
			body: submissionWithPlacements(
				placement(ids.items[0], ids.tiers[0]),
				placement(ids.items[0], ids.tiers[1]),
				placement(ids.items[1], ids.tiers[0]),
			),
		},
		{
			name: "unknown item",
			body: submissionWithPlacements(
				placement(ids.items[0], ids.tiers[0]),
				placement(ids.items[1], ids.tiers[0]),
				placement("missing-item", ids.tiers[0]),
			),
		},
		{
			name: "unknown tier",
			body: submissionWithPlacements(
				placement(ids.items[0], ids.tiers[0]),
				placement(ids.items[1], ids.tiers[0]),
				placement(ids.items[2], "missing-tier"),
			),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wantError(t, e.do(t, http.MethodPost, path, "", tt.body), http.StatusUnprocessableEntity, "validation_failed")
		})
	}

	rec := e.do(t, http.MethodGet, "/tierlists/"+ids.slug, "", "")
	if got := decodeMap(t, rec)["submissions_count"]; got != float64(0) {
		t.Fatalf("failed submissions were stored: %v", got)
	}
}

func TestReplaceUpdatesStatsAndKeepsIdentifiersPrivate(t *testing.T) {
	e := newEnv(t, nil)
	ids := createListIDs(t, e)
	path := "/tierlists/" + ids.slug

	rec := e.do(t, http.MethodPost, path+"/submissions", "", submissionBody(ids, 0, 3))
	if rec.Code != http.StatusCreated {
		t.Fatalf("submit: %d %s", rec.Code, rec.Body.String())
	}
	cookie := anonCookie(t, rec)
	resultID := decodeMap(t, rec)["id"].(string)

	statsPath := "/results/" + resultID + "/stats"
	rec = e.do(t, http.MethodGet, statsPath, "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("stats: %d %s", rec.Code, rec.Body.String())
	}
	stats := decodeMap(t, rec)
	if stats["total_submissions"] != float64(1) || stats["insufficient_data"] != true {
		t.Fatalf("stats = %v", stats)
	}
	dist := stats["items"].([]any)[0].(map[string]any)["distribution"].([]any)
	if dist[0].(map[string]any)["count"] != float64(1) || dist[1].(map[string]any)["count"] != float64(0) {
		t.Fatalf("distribution before replace = %v", dist)
	}
	for _, forbidden := range []string{"session_id", "user_id"} {
		if strings.Contains(rec.Body.String(), forbidden) {
			t.Fatalf("stats leaks %q: %s", forbidden, rec.Body.String())
		}
	}

	rec = e.doCookie(t, http.MethodPut, path+"/submissions/mine", "", submissionBody(ids, 1, 3), cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("replace: %d %s", rec.Code, rec.Body.String())
	}
	if got := decodeMap(t, rec)["id"]; got != resultID {
		t.Fatalf("replace changed result id: %v", got)
	}

	rec = e.do(t, http.MethodGet, statsPath, "", "")
	stats = decodeMap(t, rec)
	if stats["total_submissions"] != float64(1) {
		t.Fatalf("replace added a submission: %v", stats)
	}
	dist = stats["items"].([]any)[0].(map[string]any)["distribution"].([]any)
	if dist[0].(map[string]any)["count"] != float64(0) || dist[1].(map[string]any)["count"] != float64(1) {
		t.Fatalf("distribution after replace = %v", dist)
	}
}
