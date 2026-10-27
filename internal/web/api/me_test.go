package api_test

import (
	"net/http"
	"strings"
	"testing"
)

func createAs(t *testing.T, e env, user, slug string) map[string]any {
	t.Helper()
	body := strings.Replace(createBody, `"langs"`, `"`+slug+`"`, 1)
	rec := e.do(t, http.MethodPost, "/tierlists", user, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create %s as %s: %d %s", slug, user, rec.Code, rec.Body.String())
	}
	return decodeMap(t, rec)
}

func TestMyTierlistsOnlyOwn(t *testing.T) {
	e := newEnv(t, nil)
	anna := createAs(t, e, "anna", "langs")
	bob := createAs(t, e, "bob", "bobs-list")

	wantError(t, e.do(t, http.MethodGet, "/me/tierlists", "", ""), http.StatusUnauthorized, "unauthorized")

	rec := e.do(t, http.MethodGet, "/me/tierlists", "anna", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d %s", rec.Code, rec.Body.String())
	}
	m := decodeMap(t, rec)
	items, _ := m["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("anna sees %d lists: %v", len(items), m)
	}
	got := items[0].(map[string]any)
	if got["id"] != anna["id"] || got["status"] != "open" || got["submissions_count"] != float64(0) {
		t.Errorf("unexpected row: %v", got)
	}
	if m["next_cursor"] != nil {
		t.Errorf("next_cursor = %v, want null", m["next_cursor"])
	}

	rec = e.do(t, http.MethodGet, "/me/tierlists", "bob", "")
	bobs, _ := decodeMap(t, rec)["items"].([]any)
	if len(bobs) != 1 || bobs[0].(map[string]any)["id"] != bob["id"] {
		t.Fatalf("bob sees %v", bobs)
	}
}

func TestManageOwnedTierlistByID(t *testing.T) {
	e := newEnv(t, nil)
	created := createAs(t, e, "anna", "langs")
	id := created["id"].(string)
	createAs(t, e, "bob", "bobs-list")

	// Foreign and unknown IDs look the same.
	wantError(t, e.do(t, http.MethodPatch, "/tierlists/"+id, "bob", `{"status":"closed"}`), http.StatusNotFound, "not_found")
	if got := decodeMap(t, e.do(t, http.MethodGet, "/tierlists/langs", "", "")); got["status"] != "open" {
		t.Fatalf("foreign patch changed status: %v", got)
	}

	rec := e.do(t, http.MethodPatch, "/tierlists/"+id, "anna", `{"status":"closed"}`)
	if rec.Code != http.StatusOK || decodeMap(t, rec)["status"] != "closed" {
		t.Fatalf("close: %d %s", rec.Code, rec.Body.String())
	}
	if got := decodeMap(t, e.do(t, http.MethodGet, "/tierlists/langs", "", "")); got["status"] != "closed" {
		t.Fatalf("closed list not visible as closed: %v", got)
	}

	rec = e.do(t, http.MethodPatch, "/tierlists/"+id, "anna", `{"status":"public"}`)
	if rec.Code != http.StatusOK || decodeMap(t, rec)["status"] != "open" {
		t.Fatalf("reopen: %d %s", rec.Code, rec.Body.String())
	}

	wantError(t, e.do(t, http.MethodDelete, "/tierlists/"+id, "bob", ""), http.StatusNotFound, "not_found")
	if rec := e.do(t, http.MethodDelete, "/tierlists/"+id, "anna", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body.String())
	}
	wantError(t, e.do(t, http.MethodGet, "/tierlists/langs", "", ""), http.StatusNotFound, "not_found")
	wantError(t, e.do(t, http.MethodDelete, "/tierlists/"+id, "anna", ""), http.StatusNotFound, "not_found")
}

func TestPatchRejectsUnknownStatus(t *testing.T) {
	e := newEnv(t, nil)
	created := createAs(t, e, "anna", "langs")
	wantError(t, e.do(t, http.MethodPatch, "/tierlists/"+created["id"].(string), "anna", `{"status":"hidden"}`),
		http.StatusBadRequest, "bad_request")
}
