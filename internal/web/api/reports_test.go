package api_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"code.sarnas.ru/tierl/internal/domain"
)

func TestReportEndpoints(t *testing.T) {
	e := newEnv(t, nil)
	if rec := e.do(t, http.MethodPost, "/tierlists", "anna", createBody); rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}

	rec := e.do(t, http.MethodPost, "/tierlists/langs/reports", "bob", `{"reason":"spam","comment":"Duplicate content"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("report: %d %s", rec.Code, rec.Body.String())
	}
	m := decodeMap(t, rec)
	if m["id"] == "" || m["tierlist_id"] == "" || m["tierlist_slug"] != "langs" {
		t.Errorf("report identifiers = %v", m)
	}
	if m["reason"] != "spam" || m["comment"] != "Duplicate content" {
		t.Errorf("report body = %v", m)
	}
	if m["reports_count"] != float64(1) || m["hidden"] != false {
		t.Errorf("report state = %v", m)
	}
	if _, err := time.Parse(time.RFC3339, m["created_at"].(string)); err != nil {
		t.Errorf("created_at: %v", err)
	}

	// The same account cannot report the same tier list twice.
	wantError(t, e.do(t, http.MethodPost, "/tierlists/langs/reports", "bob", `{"reason":"spam"}`),
		http.StatusConflict, "report_duplicate")

	// Validation errors are reported before the tier list is inspected.
	rec = e.do(t, http.MethodPost, "/tierlists/langs/reports", "carol",
		`{"reason":"`+strings.Repeat("a", 101)+`"}`)
	wantError(t, rec, http.StatusUnprocessableEntity, "validation_failed")

	// Unlisted tier lists are not a moderation target and are hidden as missing.
	unlisted := strings.Replace(createBody, `"public"`, `"unlisted"`, 1)
	rec = e.do(t, http.MethodPost, "/tierlists", "anna", unlisted)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create unlisted: %d %s", rec.Code, rec.Body.String())
	}
	unlistedSlug := decodeMap(t, rec)["slug"].(string)
	wantError(t, e.do(t, http.MethodPost, "/tierlists/"+unlistedSlug+"/reports", "bob", `{"reason":"spam"}`),
		http.StatusNotFound, "not_found")

	// A hidden tier list is reported as missing as well.
	tl, err := e.store.GetBySlug(t.Context(), "langs")
	if err != nil {
		t.Fatal(err)
	}
	tl.Status = domain.StatusHidden
	e.store.Put(*tl)
	wantError(t, e.do(t, http.MethodPost, "/tierlists/langs/reports", "carol", `{"reason":"spam"}`),
		http.StatusNotFound, "not_found")

	// Unknown slugs and unlisted/hidden tier lists are indistinguishable.
	wantError(t, e.do(t, http.MethodPost, "/tierlists/missing/reports", "bob", `{"reason":"spam"}`),
		http.StatusNotFound, "not_found")
}
