package api_test

import (
	"encoding/json"
	"math"
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
		name   string
		body   string
		status int
		code   string
	}{
		{
			name:   "missing placements",
			body:   `{}`,
			status: http.StatusBadRequest,
			code:   "bad_request",
		},
		{
			name:   "empty placements",
			body:   submissionWithPlacements(),
			status: http.StatusBadRequest,
			code:   "bad_request",
		},
		{
			name:   "one placement",
			body:   submissionWithPlacements(placement(ids.items[0], ids.tiers[0])),
			status: http.StatusBadRequest,
			code:   "bad_request",
		},
		{
			name: "two placements",
			body: submissionWithPlacements(
				placement(ids.items[0], ids.tiers[0]),
				placement(ids.items[1], ids.tiers[0]),
			),
			status: http.StatusBadRequest,
			code:   "bad_request",
		},
		{
			name: "duplicate item",
			body: submissionWithPlacements(
				placement(ids.items[0], ids.tiers[0]),
				placement(ids.items[0], ids.tiers[1]),
				placement(ids.items[1], ids.tiers[0]),
			),
			status: http.StatusUnprocessableEntity,
			code:   "validation_failed",
		},
		{
			name: "unknown item",
			body: submissionWithPlacements(
				placement(ids.items[0], ids.tiers[0]),
				placement(ids.items[1], ids.tiers[0]),
				placement("missing-item", ids.tiers[0]),
			),
			status: http.StatusUnprocessableEntity,
			code:   "validation_failed",
		},
		{
			name: "unknown tier",
			body: submissionWithPlacements(
				placement(ids.items[0], ids.tiers[0]),
				placement(ids.items[1], ids.tiers[0]),
				placement(ids.items[2], "missing-tier"),
			),
			status: http.StatusUnprocessableEntity,
			code:   "validation_failed",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wantError(t, e.do(t, http.MethodPost, path, "", tt.body), tt.status, tt.code)
		})
	}

	rec := e.do(t, http.MethodGet, "/tierlists/"+ids.slug, "", "")
	if got := decodeMap(t, rec)["submissions_count"]; got != float64(0) {
		t.Fatalf("failed submissions were stored: %v", got)
	}

	rec = e.do(t, http.MethodPost, path, "", submissionBody(ids, 0, 3))
	if rec.Code != http.StatusCreated {
		t.Fatalf("minimum placements: %d %s", rec.Code, rec.Body.String())
	}
	rec = e.do(t, http.MethodGet, "/tierlists/"+ids.slug, "", "")
	if got := decodeMap(t, rec)["submissions_count"]; got != float64(1) {
		t.Fatalf("minimum submission was not stored: %v", got)
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

	rec = e.do(t, http.MethodGet, "/results/"+resultID, "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("result: %d %s", rec.Code, rec.Body.String())
	}
	result := decodeMap(t, rec)
	if sub, _ := result["submission"].(map[string]any); sub["id"] != resultID || sub["tierlist_slug"] != ids.slug {
		t.Fatalf("result submission = %v", result["submission"])
	}
	if tl, _ := result["tierlist"].(map[string]any); tl["slug"] != ids.slug {
		t.Fatalf("result tierlist = %v", result["tierlist"])
	}
	for _, forbidden := range []string{"session_id", "user_id"} {
		if strings.Contains(rec.Body.String(), forbidden) {
			t.Fatalf("result leaks %q: %s", forbidden, rec.Body.String())
		}
	}

	statsPath := "/results/" + resultID + "/stats"
	rec = e.do(t, http.MethodGet, statsPath, "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("stats: %d %s", rec.Code, rec.Body.String())
	}
	stats := decodeMap(t, rec)
	if stats["total_submissions"] != float64(1) || stats["insufficient_data"] != true {
		t.Fatalf("stats = %v", stats)
	}
	assertTierDistribution(t, stats, ids, 1, tierCounts(ids, 0, 1))
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
	assertTierDistribution(t, stats, ids, 1, tierCounts(ids, 1, 1))
}

func tierCounts(ids listIDs, tier, count int) map[string]map[string]int {
	out := make(map[string]map[string]int, len(ids.items))
	for _, itemID := range ids.items {
		byTier := make(map[string]int, len(ids.tiers))
		for i, tierID := range ids.tiers {
			if i == tier {
				byTier[tierID] = count
			} else {
				byTier[tierID] = 0
			}
		}
		out[itemID] = byTier
	}
	return out
}

func assertTierDistribution(t *testing.T, stats map[string]any, ids listIDs, total int, want map[string]map[string]int) {
	t.Helper()
	items, _ := stats["items"].([]any)
	if len(items) != len(ids.items) {
		t.Fatalf("items = %v", stats["items"])
	}
	seen := make(map[string]bool, len(items))
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		itemID, _ := item["item_id"].(string)
		wantTiers, ok := want[itemID]
		if !ok {
			t.Fatalf("unexpected item %q in distribution", itemID)
		}
		seen[itemID] = true

		dist, _ := item["distribution"].([]any)
		if len(dist) != len(ids.tiers) {
			t.Fatalf("item %s distribution = %v", itemID, dist)
		}
		gotTiers := make(map[string]bool, len(dist))
		for _, rawCount := range dist {
			tc, _ := rawCount.(map[string]any)
			tierID, _ := tc["tier_id"].(string)
			wantCount, ok := wantTiers[tierID]
			if !ok {
				t.Fatalf("item %s has unexpected tier %s", itemID, tierID)
			}
			gotTiers[tierID] = true
			if tc["count"] != float64(wantCount) {
				t.Fatalf("item %s tier %s count = %v, want %d", itemID, tierID, tc["count"], wantCount)
			}
			wantPercent := 0.0
			if total > 0 {
				wantPercent = math.Round(float64(wantCount)*1000/float64(total)) / 10
			}
			if tc["percent"] != wantPercent {
				t.Fatalf("item %s tier %s percent = %v, want %v", itemID, tierID, tc["percent"], wantPercent)
			}
		}
		for _, tierID := range ids.tiers {
			if !gotTiers[tierID] {
				t.Fatalf("item %s has no tier %s in distribution", itemID, tierID)
			}
		}
	}
	for itemID := range want {
		if !seen[itemID] {
			t.Fatalf("item %s is missing from distribution", itemID)
		}
	}
}

func TestResultStatsInsufficientDataThreshold(t *testing.T) {
	e := newEnv(t, nil)
	ids := createListIDs(t, e)
	path := "/tierlists/" + ids.slug + "/submissions"

	var resultID string
	for i := 0; i < 19; i++ {
		rec := e.do(t, http.MethodPost, path, "", submissionBody(ids, 0, 3))
		if rec.Code != http.StatusCreated {
			t.Fatalf("submit %d: %d %s", i, rec.Code, rec.Body.String())
		}
		if i == 0 {
			resultID = decodeMap(t, rec)["id"].(string)
		}
	}

	statsPath := "/results/" + resultID + "/stats"
	rec := e.do(t, http.MethodGet, statsPath, "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("stats at 19 answers: %d %s", rec.Code, rec.Body.String())
	}
	stats := decodeMap(t, rec)
	if stats["total_submissions"] != float64(19) || stats["insufficient_data"] != true ||
		stats["min_submissions"] != float64(20) || stats["like_you_percent"] != nil {
		t.Fatalf("19 answers stats = %v", stats)
	}
	if len(stats["similar"].([]any)) != 0 || len(stats["dissimilar"].([]any)) != 0 {
		t.Fatalf("similar/dissimilar must be empty: %v / %v", stats["similar"], stats["dissimilar"])
	}
	assertTierDistribution(t, stats, ids, 19, tierCounts(ids, 0, 19))

	rec = e.do(t, http.MethodPost, path, "", submissionBody(ids, 0, 3))
	if rec.Code != http.StatusCreated {
		t.Fatalf("20th submit: %d %s", rec.Code, rec.Body.String())
	}
	rec = e.do(t, http.MethodGet, statsPath, "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("stats at 20 answers: %d %s", rec.Code, rec.Body.String())
	}
	stats = decodeMap(t, rec)
	if stats["total_submissions"] != float64(20) || stats["insufficient_data"] != false ||
		stats["like_you_percent"] != float64(100) {
		t.Fatalf("20 answers stats = %v", stats)
	}
	assertTierDistribution(t, stats, ids, 20, tierCounts(ids, 0, 20))
	for _, forbidden := range []string{"session_id", "user_id"} {
		if strings.Contains(rec.Body.String(), forbidden) {
			t.Fatalf("stats leaks %q: %s", forbidden, rec.Body.String())
		}
	}
}
