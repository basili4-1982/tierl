package app

import (
	"errors"
	"net/http"
	"testing"

	"code.sarnas.ru/tierl/internal/storage/postgres"
	"code.sarnas.ru/tierl/internal/tierlist"
)

func TestOGResultReaderLoadsSubmission(t *testing.T) {
	f := newMVPFixture(t)
	authorID := f.confirmAuthor(t)
	created := f.create(t, authorID, mvpBody)
	listID := mvpString(t, created, "id")
	slug := mvpString(t, created, "slug")

	code, shared := f.do(t, http.MethodGet, "/api/v1/tierlists/"+slug, "", "")
	if code != http.StatusOK {
		t.Fatalf("shared tier list: %d %v", code, shared)
	}
	items := mvpObjects(t, shared, "items")
	tiers := mvpObjects(t, shared, "tiers")
	placements := mvpPlacements(t, items, tiers, 1)

	code, saved, _ := f.doCookie(t, http.MethodPost,
		"/api/v1/tierlists/"+slug+"/submissions", "", mvpSubmissionBody(t, placements), nil)
	if code != http.StatusCreated {
		t.Fatalf("submit: %d %v", code, saved)
	}
	resultID := mvpString(t, saved, "id")

	reader := ogResultReader{
		pool:  f.pool,
		store: f.store,
		users: mvpUsers{store: postgres.NewUserStore(f.pool)},
	}
	res, err := reader.GetOGResult(f.ctx, resultID)
	if err != nil {
		t.Fatalf("GetOGResult: %v", err)
	}
	if res.ID != resultID || res.Tierlist == nil || res.Tierlist.ID != listID {
		t.Fatalf("result = %+v, want submission %q of tier list %q", res, resultID, listID)
	}
	if res.AuthorName != "Anna" {
		t.Errorf("AuthorName = %q, want Anna", res.AuthorName)
	}
	if len(res.Placements) != len(placements) {
		t.Errorf("placements = %d, want %d", len(res.Placements), len(placements))
	}
	got := make(map[string]string, len(res.Placements))
	for _, placement := range res.Placements {
		got[placement.ItemID] = placement.TierID
	}
	for _, placement := range placements {
		if got[placement.ItemID] != placement.TierID {
			t.Errorf("item %s tier = %q, want %q", placement.ItemID, got[placement.ItemID], placement.TierID)
		}
	}
}

func TestOGResultReaderMissing(t *testing.T) {
	f := newMVPFixture(t)
	reader := ogResultReader{
		pool:  f.pool,
		store: f.store,
		users: mvpUsers{store: postgres.NewUserStore(f.pool)},
	}
	if _, err := reader.GetOGResult(f.ctx, "00000000-0000-4000-8000-000000000000"); !errors.Is(err, tierlist.ErrNotFound) {
		t.Fatalf("GetOGResult error = %v, want ErrNotFound", err)
	}
}
