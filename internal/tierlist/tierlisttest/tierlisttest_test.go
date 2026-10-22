package tierlisttest_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/basili4-1982/tierl/internal/domain"
	"github.com/basili4-1982/tierl/internal/tierlist"
	"github.com/basili4-1982/tierl/internal/tierlist/tierlisttest"
)

func sample(id, slug string) *domain.Tierlist {
	return &domain.Tierlist{
		ID: id, Slug: slug, AuthorID: "a", Title: "t", Status: domain.StatusOpen,
		Items: []domain.Item{{ID: "i1", Label: "x"}},
		Tiers: []domain.Tier{{ID: "t1", Name: "S", Color: "#000000"}},
	}
}

func TestMemStore(t *testing.T) {
	ctx := context.Background()
	s := tierlisttest.NewMemStore()

	if err := s.Create(ctx, sample("1", "one")); err != nil {
		t.Fatal(err)
	}
	if err := s.Create(ctx, sample("2", "one")); !errors.Is(err, tierlist.ErrSlugTaken) {
		t.Fatalf("dup slug: %v", err)
	}
	if got, err := s.GetBySlug(ctx, "one"); err != nil || got.ID != "1" {
		t.Fatalf("GetBySlug: %v %v", got, err)
	}
	if got, err := s.GetByID(ctx, "1"); err != nil || got.Slug != "one" {
		t.Fatalf("GetByID: %v %v", got, err)
	}
	if _, err := s.GetBySlug(ctx, "none"); !errors.Is(err, tierlist.ErrNotFound) {
		t.Fatalf("GetBySlug missing: %v", err)
	}
	if _, err := s.GetByID(ctx, "none"); !errors.Is(err, tierlist.ErrNotFound) {
		t.Fatalf("GetByID missing: %v", err)
	}

	s.Put(*sample("2", "two"))
	other := sample("2", "one")
	if err := s.Update(ctx, other); !errors.Is(err, tierlist.ErrSlugTaken) {
		t.Fatalf("update slug clash: %v", err)
	}
	if err := s.Update(ctx, sample("9", "nine")); !errors.Is(err, tierlist.ErrNotFound) {
		t.Fatalf("update missing: %v", err)
	}
	upd := sample("1", "one")
	upd.Title = "changed"
	if err := s.Update(ctx, upd); err != nil {
		t.Fatalf("update: %v", err)
	}

	s.SetSubmissions("1", 2)
	upd.Items = append(upd.Items, domain.Item{ID: "i2", Label: "y"})
	if err := s.Update(ctx, upd); !errors.Is(err, domain.ErrStructureLocked) {
		t.Fatalf("locked: %v", err)
	}
	if n, err := s.CountSubmissions(ctx, "1"); err != nil || n != 2 {
		t.Fatalf("count: %d %v", n, err)
	}
	if _, err := s.CountSubmissions(ctx, "none"); !errors.Is(err, tierlist.ErrNotFound) {
		t.Fatalf("count missing: %v", err)
	}

	if err := s.SetStatus(ctx, "1", domain.StatusClosed, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := s.SetStatus(ctx, "none", domain.StatusClosed, time.Now()); !errors.Is(err, tierlist.ErrNotFound) {
		t.Fatalf("SetStatus missing: %v", err)
	}
	if page, err := s.ListPublic(ctx, tierlist.PageParams{}); err != nil || len(page.Entries) != 0 {
		t.Fatalf("ListPublic: %v %v", page, err)
	}

	if err := s.Delete(ctx, "1"); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, "1"); !errors.Is(err, tierlist.ErrNotFound) {
		t.Fatalf("delete twice: %v", err)
	}
	if s.Len() != 1 {
		t.Errorf("Len = %d", s.Len())
	}

	boom := errors.New("boom")
	for _, m := range []string{"Create", "GetBySlug", "GetByID", "Update", "SetStatus", "Delete", "CountSubmissions"} {
		s.Errs[m] = boom
	}
	checks := []error{
		s.Create(ctx, sample("3", "three")),
		s.Update(ctx, sample("2", "two")),
		s.SetStatus(ctx, "2", domain.StatusClosed, time.Now()),
		s.Delete(ctx, "2"),
	}
	_, e1 := s.GetBySlug(ctx, "two")
	_, e2 := s.GetByID(ctx, "2")
	_, e3 := s.CountSubmissions(ctx, "2")
	for i, err := range append(checks, e1, e2, e3) {
		if !errors.Is(err, boom) {
			t.Errorf("check %d: %v", i, err)
		}
	}
}

func TestUsers(t *testing.T) {
	u := tierlisttest.Users{"a": {DisplayName: "A"}}
	if p, err := u.Profile(context.Background(), "a"); err != nil || p.DisplayName != "A" {
		t.Fatalf("%v %v", p, err)
	}
	if _, err := u.Profile(context.Background(), "b"); !errors.Is(err, tierlist.ErrNotFound) {
		t.Fatalf("%v", err)
	}
}
