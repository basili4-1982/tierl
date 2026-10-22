package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/basili4-1982/tierl/internal/domain"
	"github.com/basili4-1982/tierl/internal/similarity"
	"github.com/basili4-1982/tierl/internal/storage/postgres"
	"github.com/basili4-1982/tierl/internal/tierlist"
)

var t0 = time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)

func mk(id, slug string, vis domain.Visibility, status domain.Status, created time.Time) *domain.Tierlist {
	t := &domain.Tierlist{
		ID: id, Slug: slug, AuthorID: "u1", Title: "Title " + id, Description: "d",
		Visibility: vis, Status: status, CreatedAt: created, UpdatedAt: created,
	}
	for i := range 4 {
		t.Items = append(t.Items, domain.Item{ID: fmt.Sprintf("i%d", i), Label: fmt.Sprintf("Item %d", i), Position: i})
	}
	t.Items[1].ImageURL = "https://x/y.png"
	for i, n := range []string{"S", "A", "B"} {
		t.Tiers = append(t.Tiers, domain.Tier{ID: "t" + n, Name: n, Color: "#ff7f7f", Position: i})
	}
	return t
}

func place(pairs ...string) []tierlist.Placement {
	var out []tierlist.Placement
	for i := 0; i < len(pairs); i += 2 {
		out = append(out, tierlist.Placement{ItemID: pairs[i], TierID: pairs[i+1]})
	}
	return out
}

func TestCreateGetUpdateDelete(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	tl := mk("a", "alpha-list", domain.VisibilityPublic, domain.StatusDraft, t0)
	if err := s.Create(ctx, tl); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetBySlug(ctx, "alpha-list")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "a" || len(got.Items) != 4 || len(got.Tiers) != 3 || got.Items[1].ImageURL != "https://x/y.png" ||
		got.Items[0].ImageURL != "" || !got.CreatedAt.Equal(t0) || got.Status != domain.StatusDraft {
		t.Fatalf("unexpected: %+v", got)
	}
	if byID, err := s.GetByID(ctx, "a"); err != nil || byID.Slug != "alpha-list" {
		t.Fatalf("GetByID: %v %+v", err, byID)
	}
	if _, err := s.GetBySlug(ctx, "missing"); !errors.Is(err, tierlist.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}

	// Slug collision is a typed error and leaves nothing behind.
	dup := mk("b", "alpha-list", domain.VisibilityPublic, domain.StatusDraft, t0)
	if err := s.Create(ctx, dup); !errors.Is(err, tierlist.ErrSlugTaken) {
		t.Fatalf("want ErrSlugTaken, got %v", err)
	}
	if _, err := s.GetByID(ctx, "b"); !errors.Is(err, tierlist.ErrNotFound) {
		t.Fatalf("collision must roll back, got %v", err)
	}

	// Update text and structure while there are no answers.
	got.Title = "New"
	got.Status = domain.StatusOpen
	got.UpdatedAt = t0.Add(time.Hour)
	got.Items = append(got.Items, domain.Item{ID: "i4", Label: "Five", Position: 4})
	if err := s.Update(ctx, got); err != nil {
		t.Fatal(err)
	}
	upd, _ := s.GetByID(ctx, "a")
	if upd.Title != "New" || upd.Status != domain.StatusOpen || len(upd.Items) != 5 || !upd.UpdatedAt.Equal(t0.Add(time.Hour)) {
		t.Fatalf("not updated: %+v", upd)
	}

	// Update to a taken slug.
	other := mk("c", "other-list", domain.VisibilityPublic, domain.StatusDraft, t0)
	if err := s.Create(ctx, other); err != nil {
		t.Fatal(err)
	}
	upd.Slug = "other-list"
	if err := s.Update(ctx, upd); !errors.Is(err, tierlist.ErrSlugTaken) {
		t.Fatalf("want ErrSlugTaken, got %v", err)
	}
	if err := s.Update(ctx, mk("zzz", "zzz-list", domain.VisibilityPublic, domain.StatusDraft, t0)); !errors.Is(err, tierlist.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}

	// SetStatus.
	if err := s.SetStatus(ctx, "a", domain.StatusClosed, t0.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if c, _ := s.GetByID(ctx, "a"); c.Status != domain.StatusClosed {
		t.Fatalf("status %s", c.Status)
	}
	if err := s.SetStatus(ctx, "nope", domain.StatusClosed, t0); !errors.Is(err, tierlist.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}

	if err := s.Delete(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, "a"); !errors.Is(err, tierlist.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestDBConstraints(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	cases := map[string]func(*domain.Tierlist){
		"bad slug chars":    func(t *domain.Tierlist) { t.Slug = "a b c" },
		"short unlisted":    func(t *domain.Tierlist) { t.Visibility = domain.VisibilityUnlisted; t.Slug = "short-slug" },
		"bad status":        func(t *domain.Tierlist) { t.Status = "weird" },
		"empty title":       func(t *domain.Tierlist) { t.Title = " " },
		"dup item position": func(t *domain.Tierlist) { t.Items[1].Position = 0 },
		"bad color":         func(t *domain.Tierlist) { t.Tiers[0].Color = "red" },
		"long label":        func(t *domain.Tierlist) { t.Items[0].Label = strings.Repeat("я", 81) },
	}
	for name, mod := range cases {
		t.Run(name, func(t *testing.T) {
			tl := mk("x", "valid-slug", domain.VisibilityPublic, domain.StatusDraft, t0)
			mod(tl)
			if err := s.Create(ctx, tl); !errors.Is(err, domain.ErrInvalid) {
				t.Fatalf("want ErrInvalid, got %v", err)
			}
		})
	}
}

func TestSaveAndCounters(t *testing.T) {
	s, pool := newStore(t)
	ctx := context.Background()
	if err := s.Create(ctx, mk("a", "alpha-list", domain.VisibilityPublic, domain.StatusOpen, t0)); err != nil {
		t.Fatal(err)
	}

	r1, err := s.Save(ctx, tierlist.Submission{ID: "s1", TierlistID: "a", SessionID: "sess1",
		Placements: place("i0", "tS", "i1", "tA", "i2", "tB")}, t0)
	if err != nil || r1.Replaced || r1.SubmissionID != "s1" {
		t.Fatalf("first save: %v %+v", err, r1)
	}
	if _, err := s.Save(ctx, tierlist.Submission{ID: "s2", TierlistID: "a", SessionID: "sess2",
		Placements: place("i0", "tS", "i1", "tB", "i3", "tA")}, t0); err != nil {
		t.Fatal(err)
	}

	dist, err := s.Distributions(ctx, "a")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]int{"i0": {2, 0, 0}, "i1": {0, 1, 1}, "i2": {0, 0, 1}, "i3": {0, 1, 0}}
	if len(dist) != 4 {
		t.Fatalf("dist len %d", len(dist))
	}
	for _, d := range dist {
		if fmt.Sprint(d.Counts) != fmt.Sprint(want[d.ItemID]) {
			t.Errorf("%s: %v want %v", d.ItemID, d.Counts, want[d.ItemID])
		}
	}

	// Replace: the same session sends a different answer; counters follow and the ID is kept.
	r, err := s.Save(ctx, tierlist.Submission{ID: "other-id", TierlistID: "a", SessionID: "sess1",
		Placements: place("i0", "tB", "i3", "tS", "i2", "tB")}, t0.Add(time.Minute))
	if err != nil || !r.Replaced || r.SubmissionID != "s1" {
		t.Fatalf("replace: %v %+v", err, r)
	}
	dist, _ = s.Distributions(ctx, "a")
	want = map[string][]int{"i0": {1, 0, 1}, "i1": {0, 0, 1}, "i2": {0, 0, 1}, "i3": {1, 1, 0}}
	for _, d := range dist {
		if fmt.Sprint(d.Counts) != fmt.Sprint(want[d.ItemID]) {
			t.Errorf("after replace %s: %v want %v", d.ItemID, d.Counts, want[d.ItemID])
		}
	}

	// The pre-aggregate always equals a recount from placements.
	var mismatches int
	err = pool.QueryRow(ctx, `
		SELECT count(*) FROM (
			SELECT item_id, tier_id, count(*) AS n FROM placements GROUP BY item_id, tier_id
		) p FULL JOIN item_tier_counts c USING (item_id, tier_id)
		WHERE COALESCE(p.n, 0) <> COALESCE(c.count, 0)`).Scan(&mismatches)
	if err != nil || mismatches != 0 {
		t.Fatalf("counters drifted: %v mismatches=%d", err, mismatches)
	}

	if n, err := s.CountSubmissions(ctx, "a"); err != nil || n != 2 {
		t.Fatalf("CountSubmissions %d %v", n, err)
	}
	if _, err := s.CountSubmissions(ctx, "nope"); !errors.Is(err, tierlist.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}

	answers, err := s.Answers(ctx, "a")
	if err != nil || len(answers) != 2 {
		t.Fatalf("answers: %v %v", err, answers)
	}
	if answers[0].ID != "s1" {
		t.Errorf("order: %+v", answers)
	}
	wantA := similarity.Answer{"i0": 2, "i3": 0, "i2": 2}
	if fmt.Sprint(answers[0].Answer) != fmt.Sprint(wantA) {
		t.Errorf("answer %v want %v", answers[0].Answer, wantA)
	}
	if got, _ := s.Answers(ctx, "nope"); len(got) != 0 {
		t.Errorf("unexpected answers %v", got)
	}

	// Structure is locked once there are answers.
	tl, _ := s.GetByID(ctx, "a")
	tl.Items = tl.Items[:3]
	if err := s.Update(ctx, tl); !errors.Is(err, domain.ErrStructureLocked) {
		t.Fatalf("want ErrStructureLocked, got %v", err)
	}
	tl, _ = s.GetByID(ctx, "a")
	tl.Title = "Retitled"
	if err := s.Update(ctx, tl); err != nil {
		t.Fatalf("text edit after answers: %v", err)
	}

	// Deleting the tier list removes all dependent rows.
	if err := s.Delete(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	var rows int
	_ = pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM placements) + (SELECT count(*) FROM item_tier_counts) + (SELECT count(*) FROM submissions)`).Scan(&rows)
	if rows != 0 {
		t.Fatalf("%d orphan rows", rows)
	}
}

func TestSaveErrors(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	if err := s.Create(ctx, mk("a", "alpha-list", domain.VisibilityPublic, domain.StatusOpen, t0)); err != nil {
		t.Fatal(err)
	}
	if err := s.Create(ctx, mk("d", "draft-list", domain.VisibilityPublic, domain.StatusDraft, t0)); err != nil {
		t.Fatal(err)
	}
	sub := func(tl string, p ...string) tierlist.Submission {
		return tierlist.Submission{ID: "s", TierlistID: tl, SessionID: "x", Placements: place(p...)}
	}

	if _, err := s.Save(ctx, sub("nope", "i0", "tS"), t0); !errors.Is(err, tierlist.ErrNotFound) {
		t.Errorf("unknown tierlist: %v", err)
	}
	if _, err := s.Save(ctx, sub("d", "i0", "tS"), t0); !errors.Is(err, tierlist.ErrNotAcceptingSubmissions) {
		t.Errorf("draft: %v", err)
	}
	if _, err := s.Save(ctx, sub("a", "i0", "tS", "i0", "tA"), t0); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("duplicate item: %v", err)
	}
	if _, err := s.Save(ctx, sub("a", "ghost", "tS"), t0); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("unknown item: %v", err)
	}
	if _, err := s.Save(ctx, sub("a", "i0", "ghost"), t0); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("unknown tier: %v", err)
	}
	// A failed save leaves no submission behind.
	if n, _ := s.CountSubmissions(ctx, "a"); n != 0 {
		t.Errorf("rollback failed, %d submissions", n)
	}

	// Closing stops answers.
	if err := s.SetStatus(ctx, "a", domain.StatusClosed, t0); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Save(ctx, sub("a", "i0", "tS"), t0); !errors.Is(err, tierlist.ErrNotAcceptingSubmissions) {
		t.Errorf("closed: %v", err)
	}
}

func TestConcurrentSaveSameSession(t *testing.T) {
	s, pool := newStore(t)
	ctx := context.Background()
	if err := s.Create(ctx, mk("a", "alpha-list", domain.VisibilityPublic, domain.StatusOpen, t0)); err != nil {
		t.Fatal(err)
	}
	errs := make(chan error, 8)
	for i := range 8 {
		go func() {
			tier := []string{"tS", "tA", "tB"}[i%3]
			_, err := s.Save(ctx, tierlist.Submission{ID: fmt.Sprintf("s%d", i), TierlistID: "a", SessionID: "same",
				Placements: place("i0", tier, "i1", tier, "i2", tier)}, t0)
			errs <- err
		}()
	}
	for range 8 {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	var subs, total int
	_ = pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM submissions), (SELECT COALESCE(sum(count), 0) FROM item_tier_counts)`).Scan(&subs, &total)
	if subs != 1 || total != 3 {
		t.Fatalf("submissions=%d counter total=%d, want 1 and 3", subs, total)
	}
}

func TestListPublic(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	// 5 public+open lists; two share created_at to exercise the id tiebreaker.
	for i, c := range []time.Time{t0, t0.Add(time.Minute), t0.Add(time.Minute), t0.Add(2 * time.Minute), t0.Add(3 * time.Minute)} {
		if err := s.Create(ctx, mk(fmt.Sprintf("p%d", i), fmt.Sprintf("public-%d", i), domain.VisibilityPublic, domain.StatusOpen, c)); err != nil {
			t.Fatal(err)
		}
	}
	// Excluded: unlisted, draft, closed, hidden.
	hidden := []*domain.Tierlist{
		mk("u", "unlisted-slug-0001", domain.VisibilityUnlisted, domain.StatusOpen, t0),
		mk("d", "draft-list", domain.VisibilityPublic, domain.StatusDraft, t0),
		mk("c", "closed-list", domain.VisibilityPublic, domain.StatusClosed, t0),
		mk("h", "hidden-list", domain.VisibilityPublic, domain.StatusHidden, t0),
	}
	for _, h := range hidden {
		if err := s.Create(ctx, h); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Save(ctx, tierlist.Submission{ID: "s", TierlistID: "p4", SessionID: "x", Placements: place("i0", "tS")}, t0); err != nil {
		t.Fatal(err)
	}

	var ids []string
	cursor := ""
	for pages := 0; ; pages++ {
		page, err := s.ListPublic(ctx, tierlist.PageParams{Limit: 2, Cursor: cursor})
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range page.Entries {
			ids = append(ids, e.ID)
			if e.ID == "p4" && (e.Submissions != 1 || e.ItemCount != 4) {
				t.Errorf("counts: %+v", e)
			}
		}
		if page.NextCursor == "" {
			if pages != 2 {
				t.Errorf("pages=%d want 3", pages+1)
			}
			break
		}
		cursor = page.NextCursor
	}
	if fmt.Sprint(ids) != "[p4 p3 p2 p1 p0]" {
		t.Fatalf("order %v", ids)
	}

	// Default and clamped page sizes.
	for _, l := range []int{0, 1000} {
		page, err := s.ListPublic(ctx, tierlist.PageParams{Limit: l})
		if err != nil || len(page.Entries) != 5 || page.NextCursor != "" {
			t.Errorf("limit %d: %v %+v", l, err, page)
		}
	}
	for _, bad := range []string{"!!!", "bm9wZQ", "YXxi", "MXw"} {
		if _, err := s.ListPublic(ctx, tierlist.PageParams{Cursor: bad}); !errors.Is(err, tierlist.ErrBadCursor) {
			t.Errorf("cursor %q: %v", bad, err)
		}
	}
}

func TestMigrations(t *testing.T) {
	newStore(t) // down + up
	if err := postgres.MigrateUp(testURL); err != nil {
		t.Fatalf("second up must be a no-op: %v", err)
	}
	if err := postgres.MigrateOnStart(false, ""); err != nil {
		t.Fatal(err)
	}
	if err := postgres.MigrateOnStart(true, ""); err == nil {
		t.Fatal("want error for empty url")
	}
	if err := postgres.MigrateOnStart(true, testURL); err != nil {
		t.Fatal(err)
	}
	if err := postgres.MigrateUp("postgres://tierl:tierl@127.0.0.1:1/none?sslmode=disable&connect_timeout=1"); err == nil {
		t.Fatal("want connection error")
	}
	if _, err := postgres.Open(context.Background(), "postgres://tierl:tierl@127.0.0.1:1/none?sslmode=disable&connect_timeout=1"); err == nil {
		t.Fatal("want connection error")
	}
}
