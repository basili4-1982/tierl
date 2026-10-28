package tierlist_test

import (
	"context"
	"errors"
	"math/rand"
	"strings"
	"testing"
	"time"

	"code.sarnas.ru/tierl/internal/domain"
	"code.sarnas.ru/tierl/internal/tierlist"
	"code.sarnas.ru/tierl/internal/tierlist/tierlisttest"
)

var fixedNow = time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)

type env struct {
	svc   *tierlist.Service
	store *tierlisttest.MemStore
}

func newEnv(t *testing.T) env {
	t.Helper()
	store := tierlisttest.NewMemStore()
	users := tierlisttest.Users{
		"anna": {DisplayName: "Anna", EmailVerified: true},
		"bob":  {DisplayName: "Bob", EmailVerified: true},
		"newb": {DisplayName: "Newbie", EmailVerified: false},
	}
	svc := tierlist.NewService(store, users, tierlist.ServiceDeps{
		Now:  func() time.Time { return fixedNow },
		Rand: rand.New(rand.NewSource(1)), //nolint:gosec // deterministic test entropy
	})
	return env{svc: svc, store: store}
}

func validCreate() tierlist.CreateInput {
	return tierlist.CreateInput{
		Title:       "Languages",
		Description: "Rank them",
		Visibility:  domain.VisibilityPublic,
		Slug:        "langs",
		Tiers:       []tierlist.TierInput{{Name: "S", Color: "#ff0000"}, {Name: "A", Color: "#00ff00"}},
		Items: []tierlist.ItemInput{
			{Label: "Go"}, {Label: "Rust", ImageURL: "https://cdn.example.com/rust.png"}, {Label: "Zig"},
		},
	}
}

func TestCreate(t *testing.T) {
	e := newEnv(t)
	d, err := e.svc.Create(context.Background(), "anna", validCreate())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	tl := d.Tierlist
	if tl.Status != domain.StatusOpen || tl.AuthorID != "anna" || tl.Slug != "langs" {
		t.Errorf("unexpected tierlist: %+v", tl)
	}
	if !d.IsOwner || d.AuthorName != "Anna" || d.SubmissionsCount != 0 || d.ClosedAt() != nil {
		t.Errorf("unexpected details: %+v", d)
	}
	if len(tl.Items) != 3 || tl.Items[1].Position != 1 || tl.Tiers[1].Position != 1 || tl.Items[0].ID == tl.Items[1].ID {
		t.Errorf("structure not built: %+v", tl)
	}
	if len(tl.ID) != 36 || tl.ID[14] != '4' {
		t.Errorf("ID %q is not a UUIDv4", tl.ID)
	}
	if e.store.Len() != 1 {
		t.Errorf("store has %d tierlists", e.store.Len())
	}
}

func TestCreate_UnlistedGetsRandomSlug(t *testing.T) {
	e := newEnv(t)
	in := validCreate()
	in.Visibility, in.Slug = domain.VisibilityUnlisted, "ignored"
	d, err := e.svc.Create(context.Background(), "anna", in)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if d.Tierlist.Slug == "ignored" || len(d.Tierlist.Slug) != domain.SlugLength {
		t.Errorf("slug = %q", d.Tierlist.Slug)
	}
}

func TestCreate_Errors(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name   string
		user   string
		mutate func(*tierlist.CreateInput)
		fail   map[string]error
		want   error
	}{
		{name: "unknown user", user: "ghost", want: tierlist.ErrNotFound},
		{name: "email not verified", user: "newb", want: tierlist.ErrEmailNotVerified},
		{name: "empty title", user: "anna", mutate: func(in *tierlist.CreateInput) { in.Title = " " }, want: domain.ErrInvalid},
		{name: "public without slug", user: "anna", mutate: func(in *tierlist.CreateInput) { in.Slug = "" }, want: domain.ErrInvalid},
		{name: "too few items", user: "anna", mutate: func(in *tierlist.CreateInput) { in.Items = in.Items[:1] }, want: domain.ErrInvalid},
		{name: "bad image scheme", user: "anna", mutate: func(in *tierlist.CreateInput) { in.Items[0].ImageURL = "javascript:alert(1)" }, want: domain.ErrInvalid},
		{name: "image without host", user: "anna", mutate: func(in *tierlist.CreateInput) { in.Items[0].ImageURL = "https://" }, want: domain.ErrInvalid},
		{name: "store failure", user: "anna", fail: map[string]error{"Create": errors.New("boom")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			for k, v := range tt.fail {
				e.store.Errs[k] = v
			}
			in := validCreate()
			if tt.mutate != nil {
				tt.mutate(&in)
			}
			_, err := e.svc.Create(ctx, tt.user, in)
			if err == nil {
				t.Fatal("expected error")
			}
			if tt.want != nil && !errors.Is(err, tt.want) {
				t.Errorf("err = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestCreate_SlugTaken(t *testing.T) {
	e := newEnv(t)
	if _, err := e.svc.Create(context.Background(), "anna", validCreate()); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Create(context.Background(), "bob", validCreate()); !errors.Is(err, tierlist.ErrSlugTaken) {
		t.Fatalf("err = %v, want ErrSlugTaken", err)
	}
}

func TestCreate_RandomFailure(t *testing.T) {
	store := tierlisttest.NewMemStore()
	svc := tierlist.NewService(store, tierlisttest.Users{"anna": {EmailVerified: true}},
		tierlist.ServiceDeps{Rand: strings.NewReader("")})
	if _, err := svc.Create(context.Background(), "anna", validCreate()); !errors.Is(err, domain.ErrRandom) {
		t.Fatalf("err = %v, want ErrRandom", err)
	}
}

func TestNewService_Defaults(t *testing.T) {
	svc := tierlist.NewService(tierlisttest.NewMemStore(), tierlisttest.Users{"anna": {EmailVerified: true}}, tierlist.ServiceDeps{})
	d, err := svc.Create(context.Background(), "anna", validCreate())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if time.Since(d.Tierlist.CreatedAt) > time.Minute {
		t.Errorf("CreatedAt = %v", d.Tierlist.CreatedAt)
	}
}

func TestGet(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	if _, err := e.svc.Create(ctx, "anna", validCreate()); err != nil {
		t.Fatal(err)
	}
	t.Run("owner", func(t *testing.T) {
		d, err := e.svc.Get(ctx, "anna", "langs")
		if err != nil || !d.IsOwner || d.AuthorName != "Anna" {
			t.Fatalf("got %+v, %v", d, err)
		}
	})
	t.Run("guest", func(t *testing.T) {
		d, err := e.svc.Get(ctx, "", "langs")
		if err != nil || d.IsOwner {
			t.Fatalf("got %+v, %v", d, err)
		}
	})
	t.Run("other user", func(t *testing.T) {
		d, err := e.svc.Get(ctx, "bob", "langs")
		if err != nil || d.IsOwner {
			t.Fatalf("got %+v, %v", d, err)
		}
	})
	t.Run("missing", func(t *testing.T) {
		if _, err := e.svc.Get(ctx, "", "nothing"); !errors.Is(err, tierlist.ErrNotFound) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("malformed slug", func(t *testing.T) {
		if _, err := e.svc.Get(ctx, "", "a b"); !errors.Is(err, tierlist.ErrNotFound) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("hidden and draft are not found", func(t *testing.T) {
		for _, st := range []domain.Status{domain.StatusHidden, domain.StatusDraft} {
			tl, _ := e.store.GetBySlug(ctx, "langs")
			tl.Status = st
			e.store.Put(*tl)
			if _, err := e.svc.Get(ctx, "anna", "langs"); !errors.Is(err, tierlist.ErrNotFound) {
				t.Fatalf("%s: err = %v", st, err)
			}
		}
	})
}

func TestGet_AuthorMissingAndStoreErrors(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	d, err := e.svc.Create(ctx, "anna", validCreate())
	if err != nil {
		t.Fatal(err)
	}
	e.store.SetSubmissions(d.Tierlist.ID, 7)

	svc := tierlist.NewService(e.store, tierlisttest.Users{}, tierlist.ServiceDeps{})
	got, err := svc.Get(ctx, "", "langs")
	if err != nil || got.AuthorName != "" || got.SubmissionsCount != 7 {
		t.Fatalf("removed author: %+v, %v", got, err)
	}

	e.store.Errs["CountSubmissions"] = errors.New("boom")
	if _, err := e.svc.Get(ctx, "", "langs"); err == nil {
		t.Fatal("expected count error")
	}
	delete(e.store.Errs, "CountSubmissions")

	failing := tierlist.NewService(e.store, failingUsers{}, tierlist.ServiceDeps{})
	if _, err := failing.Get(ctx, "", "langs"); err == nil {
		t.Fatal("expected user directory error")
	}
}

type failingUsers struct{}

func (failingUsers) Profile(context.Context, string) (tierlist.Profile, error) {
	return tierlist.Profile{}, errors.New("directory down")
}

func ptr[T any](v T) *T { return &v }

func TestUpdate_TextAndVisibility(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	if _, err := e.svc.Create(ctx, "anna", validCreate()); err != nil {
		t.Fatal(err)
	}

	d, err := e.svc.Update(ctx, "anna", "langs", tierlist.UpdateInput{Title: ptr("New"), Description: ptr("")})
	if err != nil || d.Tierlist.Title != "New" || d.Tierlist.Description != "" {
		t.Fatalf("text: %+v, %v", d, err)
	}
	d, err = e.svc.Update(ctx, "anna", "langs", tierlist.UpdateInput{Description: ptr("again")})
	if err != nil || d.Tierlist.Title != "New" || d.Tierlist.Description != "again" {
		t.Fatalf("description only: %+v, %v", d, err)
	}

	// public -> unlisted replaces the short slug with a random one.
	v := domain.VisibilityUnlisted
	d, err = e.svc.Update(ctx, "anna", "langs", tierlist.UpdateInput{Visibility: &v})
	if err != nil {
		t.Fatalf("visibility: %v", err)
	}
	slug := d.Tierlist.Slug
	if d.Tierlist.Visibility != v || len(slug) != domain.SlugLength {
		t.Fatalf("slug = %q, vis = %s", slug, d.Tierlist.Visibility)
	}
	if _, err := e.svc.Get(ctx, "", "langs"); !errors.Is(err, tierlist.ErrNotFound) {
		t.Errorf("old slug still resolves: %v", err)
	}

	// unlisted -> public keeps the slug; same visibility is a no-op.
	for _, nv := range []domain.Visibility{domain.VisibilityPublic, domain.VisibilityPublic} {
		d, err = e.svc.Update(ctx, "anna", slug, tierlist.UpdateInput{Visibility: &nv})
		if err != nil || d.Tierlist.Slug != slug || d.Tierlist.Visibility != domain.VisibilityPublic {
			t.Fatalf("to public: %+v, %v", d, err)
		}
	}
	// public (long slug) -> unlisted keeps it.
	d, err = e.svc.Update(ctx, "anna", slug, tierlist.UpdateInput{Visibility: &v})
	if err != nil || d.Tierlist.Slug != slug {
		t.Fatalf("keep long slug: %+v, %v", d, err)
	}
}

func TestUpdate_Errors(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	if _, err := e.svc.Create(ctx, "anna", validCreate()); err != nil {
		t.Fatal(err)
	}
	bad := domain.Visibility("secret")
	tests := []struct {
		name string
		user string
		slug string
		in   tierlist.UpdateInput
		want error
	}{
		{"guest", "", "langs", tierlist.UpdateInput{Title: ptr("x")}, tierlist.ErrForbidden},
		{"not author", "bob", "langs", tierlist.UpdateInput{Title: ptr("x")}, tierlist.ErrNotFound},
		{"missing", "anna", "nothing", tierlist.UpdateInput{Title: ptr("x")}, tierlist.ErrNotFound},
		{"empty title", "anna", "langs", tierlist.UpdateInput{Title: ptr("")}, domain.ErrInvalid},
		{"long description", "anna", "langs", tierlist.UpdateInput{Description: ptr(strings.Repeat("я", 1001))}, domain.ErrInvalid},
		{"bad visibility", "anna", "langs", tierlist.UpdateInput{Visibility: &bad}, domain.ErrInvalid},
		{"too few items", "anna", "langs", tierlist.UpdateInput{Items: []tierlist.ItemInput{{Label: "a"}}}, domain.ErrInvalid},
		{"bad image", "anna", "langs", tierlist.UpdateInput{Items: []tierlist.ItemInput{{Label: "a", ImageURL: "ftp://x/y"}, {Label: "b"}, {Label: "c"}}}, domain.ErrInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := e.svc.Update(ctx, tt.user, tt.slug, tt.in); !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
		})
	}

	e.store.Errs["Update"] = errors.New("boom")
	if _, err := e.svc.Update(ctx, "anna", "langs", tierlist.UpdateInput{Title: ptr("ok")}); err == nil {
		t.Fatal("expected store error")
	}
	delete(e.store.Errs, "Update")
	e.store.Errs["CountSubmissions"] = errors.New("boom")
	in := tierlist.UpdateInput{Items: []tierlist.ItemInput{{Label: "a"}, {Label: "b"}, {Label: "c"}}}
	if _, err := e.svc.Update(ctx, "anna", "langs", in); err == nil {
		t.Fatal("expected count error")
	}
}

func TestUpdate_Structure(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	created, err := e.svc.Create(ctx, "anna", validCreate())
	if err != nil {
		t.Fatal(err)
	}
	newItems := []tierlist.ItemInput{{Label: "A"}, {Label: "B"}, {Label: "C"}, {Label: "D"}}
	newTiers := []tierlist.TierInput{{Name: "Top", Color: "#112233"}, {Name: "Mid", Color: "#445566"}, {Name: "Low", Color: "#778899"}}

	t.Run("free before answers", func(t *testing.T) {
		d, err := e.svc.Update(ctx, "anna", "langs", tierlist.UpdateInput{Items: newItems, Tiers: newTiers})
		if err != nil || len(d.Tierlist.Items) != 4 || len(d.Tierlist.Tiers) != 3 {
			t.Fatalf("got %+v, %v", d, err)
		}
	})
	t.Run("only tiers keeps items", func(t *testing.T) {
		before, _ := e.svc.Get(ctx, "", "langs")
		d, err := e.svc.Update(ctx, "anna", "langs", tierlist.UpdateInput{Tiers: newTiers[:2]})
		if err != nil || len(d.Tierlist.Tiers) != 2 || d.Tierlist.Items[0].ID != before.Tierlist.Items[0].ID {
			t.Fatalf("got %+v, %v", d, err)
		}
	})

	e.store.SetSubmissions(created.Tierlist.ID, 3)
	t.Run("locked after first answer", func(t *testing.T) {
		_, err := e.svc.Update(ctx, "anna", "langs", tierlist.UpdateInput{Items: newItems[:3]})
		if !errors.Is(err, domain.ErrStructureLocked) {
			t.Fatalf("err = %v, want ErrStructureLocked", err)
		}
	})
	t.Run("identical structure is not a change", func(t *testing.T) {
		cur, _ := e.svc.Get(ctx, "", "langs")
		var items []tierlist.ItemInput
		for _, it := range cur.Tierlist.Items {
			items = append(items, tierlist.ItemInput{Label: it.Label, ImageURL: it.ImageURL})
		}
		var tiers []tierlist.TierInput
		for _, tr := range cur.Tierlist.Tiers {
			tiers = append(tiers, tierlist.TierInput{Name: tr.Name, Color: strings.ToUpper(tr.Color)})
		}
		d, err := e.svc.Update(ctx, "anna", "langs", tierlist.UpdateInput{Title: ptr("Renamed"), Items: items, Tiers: tiers})
		if err != nil || d.Tierlist.Title != "Renamed" || d.SubmissionsCount != 3 {
			t.Fatalf("got %+v, %v", d, err)
		}
	})
	t.Run("text still editable", func(t *testing.T) {
		if _, err := e.svc.Update(ctx, "anna", "langs", tierlist.UpdateInput{Title: ptr("Still ok")}); err != nil {
			t.Fatal(err)
		}
	})
}

func TestClose(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	if _, err := e.svc.Create(ctx, "anna", validCreate()); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Close(ctx, "bob", "langs"); !errors.Is(err, tierlist.ErrNotFound) {
		t.Fatalf("not author: %v", err)
	}
	if _, err := e.svc.Close(ctx, "anna", "nothing"); !errors.Is(err, tierlist.ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}

	d, err := e.svc.Close(ctx, "anna", "langs")
	if err != nil || d.Tierlist.Status != domain.StatusClosed || d.ClosedAt() == nil || !d.ClosedAt().Equal(fixedNow) {
		t.Fatalf("close: %+v, %v", d, err)
	}
	// Idempotent: a second call succeeds and keeps the state.
	d, err = e.svc.Close(ctx, "anna", "langs")
	if err != nil || d.Tierlist.Status != domain.StatusClosed {
		t.Fatalf("second close: %+v, %v", d, err)
	}
	// A closed tier list is still readable.
	if g, err := e.svc.Get(ctx, "", "langs"); err != nil || g.Tierlist.Status != domain.StatusClosed {
		t.Fatalf("get closed: %+v, %v", g, err)
	}
}

func TestClosedAtStableAfterTextEdit(t *testing.T) {
	ctx := context.Background()
	now := fixedNow
	store := tierlisttest.NewMemStore()
	users := tierlisttest.Users{"anna": {DisplayName: "Anna", EmailVerified: true}}
	svc := tierlist.NewService(store, users, tierlist.ServiceDeps{
		Now:  func() time.Time { return now },
		Rand: rand.New(rand.NewSource(1)), //nolint:gosec // deterministic test entropy
	})
	if _, err := svc.Create(ctx, "anna", validCreate()); err != nil {
		t.Fatal(err)
	}

	now = fixedNow.Add(time.Hour)
	closed, err := svc.Close(ctx, "anna", "langs")
	if err != nil {
		t.Fatal(err)
	}
	closedAt := closed.ClosedAt()
	if closedAt == nil || !closedAt.Equal(now) {
		t.Fatalf("closed_at after close = %v, want %v", closedAt, now)
	}

	now = fixedNow.Add(2 * time.Hour)
	edited, err := svc.Update(ctx, "anna", "langs", tierlist.UpdateInput{
		Title:       ptr("Renamed"),
		Description: ptr("New description"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if edited.Tierlist.Status != domain.StatusClosed {
		t.Fatalf("status = %s after text edit, want closed", edited.Tierlist.Status)
	}
	if got := edited.ClosedAt(); got == nil || !got.Equal(*closedAt) {
		t.Fatalf("closed_at moved after text edit: %v, want %v", got, closedAt)
	}
	if !edited.Tierlist.UpdatedAt.Equal(now) {
		t.Fatalf("updated_at = %v, want %v", edited.Tierlist.UpdatedAt, now)
	}

	stored, err := svc.Get(ctx, "", "langs")
	if err != nil {
		t.Fatal(err)
	}
	if got := stored.ClosedAt(); got == nil || !got.Equal(*closedAt) {
		t.Fatalf("stored closed_at = %v, want %v", got, closedAt)
	}

	reopened, err := svc.Update(ctx, "anna", "langs", tierlist.UpdateInput{Status: ptr(domain.StatusOpen)})
	if err != nil {
		t.Fatal(err)
	}
	if reopened.ClosedAt() != nil || reopened.Tierlist.ClosedAt != nil {
		t.Fatalf("closed_at not cleared on reopen: %v", reopened.ClosedAt())
	}
}

func TestClose_StoreFailure(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	if _, err := e.svc.Create(ctx, "anna", validCreate()); err != nil {
		t.Fatal(err)
	}
	e.store.Errs["SetStatus"] = errors.New("boom")
	if _, err := e.svc.Close(ctx, "anna", "langs"); err == nil {
		t.Fatal("expected error")
	}
}

func TestDelete(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	if _, err := e.svc.Create(ctx, "anna", validCreate()); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.Delete(ctx, "bob", "langs"); !errors.Is(err, tierlist.ErrNotFound) {
		t.Fatalf("not author: %v", err)
	}
	if err := e.svc.Delete(ctx, "", "langs"); !errors.Is(err, tierlist.ErrForbidden) {
		t.Fatalf("guest: %v", err)
	}
	if err := e.svc.Delete(ctx, "anna", "langs"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := e.svc.Delete(ctx, "anna", "langs"); !errors.Is(err, tierlist.ErrNotFound) {
		t.Fatalf("second delete: %v", err)
	}
}
