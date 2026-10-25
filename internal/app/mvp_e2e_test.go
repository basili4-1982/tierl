package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/basili4-1982/tierl/internal/auth"
	"github.com/basili4-1982/tierl/internal/domain"
	"github.com/basili4-1982/tierl/internal/similarity"
	"github.com/basili4-1982/tierl/internal/storage/postgres"
	"github.com/basili4-1982/tierl/internal/tierlist"
	"github.com/basili4-1982/tierl/internal/web"
)

const (
	mvpPassword = "correct-horse-battery"
	mvpBody     = `{"title":"MVP languages","description":"rank them","visibility":"public","slug":"mvp-public",
"tiers":[{"name":"S","color":"#ff0000"},{"name":"A","color":"#00ff00"}],
"items":[{"label":"Go"},{"label":"Rust"},{"label":"Zig"}]}`
)

type mvpUsers struct {
	store *postgres.UserStore
}

func (u mvpUsers) Profile(ctx context.Context, id string) (tierlist.Profile, error) {
	user, err := u.store.GetByID(ctx, id)
	if errors.Is(err, auth.ErrUserNotFound) {
		return tierlist.Profile{}, tierlist.ErrNotFound
	}
	if err != nil {
		return tierlist.Profile{}, err
	}
	return tierlist.Profile{DisplayName: user.DisplayName, EmailVerified: user.EmailVerified}, nil
}

type mvpFixture struct {
	ctx     context.Context
	pool    *pgxpool.Pool
	store   *postgres.Store
	auth    *auth.Service
	handler http.Handler
	author  *auth.User
}

func newMVPFixture(t *testing.T) *mvpFixture {
	t.Helper()
	url := testDB(t)
	ctx := context.Background()
	pool, err := postgres.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	userStore := postgres.NewUserStore(pool)
	authSvc, err := auth.NewService(userStore, auth.Options{
		Hash: auth.HashParams{MemoryKiB: 64, Iterations: 1, Parallelism: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	author, err := authSvc.Register(ctx, auth.RegisterInput{
		Email: "Anna@Example.COM", Password: mvpPassword, DisplayName: "Anna",
	})
	if err != nil {
		t.Fatal(err)
	}

	cfg := testConfig(url)
	cfg.CreateTierlistBurst = 10
	deps, err := newDeps(pool, cfg, discardLog(), headerAuth{}, mvpUsers{store: userStore})
	if err != nil {
		t.Fatal(err)
	}
	return &mvpFixture{
		ctx: ctx, pool: pool, store: postgres.New(pool), auth: authSvc,
		handler: web.NewHandler(deps), author: author,
	}
}

func (f *mvpFixture) confirmAuthor(t *testing.T) *auth.User {
	t.Helper()
	if _, err := f.pool.Exec(f.ctx, `
		UPDATE users SET email_verified = true, updated_at = GREATEST(updated_at, now())
		WHERE id = $1`, f.author.ID); err != nil {
		t.Fatalf("confirm email: %v", err)
	}
	user, err := f.auth.Login(f.ctx, "anna@example.com", mvpPassword)
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if !user.EmailVerified {
		t.Fatal("login returned an unverified user")
	}
	return user
}

func (f *mvpFixture) do(t *testing.T, method, path, user, payload string) (int, map[string]any) {
	t.Helper()
	return call(t, f.handler, method, path, user, payload)
}

func (f *mvpFixture) create(t *testing.T, userID, payload string) map[string]any {
	t.Helper()
	code, created := f.do(t, http.MethodPost, "/api/v1/tierlists", userID, payload)
	if code != http.StatusCreated {
		t.Fatalf("create: %d %v", code, created)
	}
	return created
}

func mvpString(t *testing.T, m map[string]any, key string) string {
	t.Helper()
	value, ok := m[key].(string)
	if !ok || value == "" {
		t.Fatalf("%s = %v, want a non-empty string", key, m[key])
	}
	return value
}

func mvpObjects(t *testing.T, m map[string]any, key string) []map[string]any {
	t.Helper()
	raw, ok := m[key].([]any)
	if !ok {
		t.Fatalf("%s = %v, want an array", key, m[key])
	}
	out := make([]map[string]any, len(raw))
	for i, value := range raw {
		obj, ok := value.(map[string]any)
		if !ok {
			t.Fatalf("%s[%d] = %v, want an object", key, i, value)
		}
		out[i] = obj
	}
	return out
}

func mvpPlacements(t *testing.T, items, tiers []map[string]any, tierIndex int) []tierlist.Placement {
	t.Helper()
	tierID := mvpString(t, tiers[tierIndex], "id")
	out := make([]tierlist.Placement, 0, len(items))
	for _, item := range items {
		out = append(out, tierlist.Placement{ItemID: mvpString(t, item, "id"), TierID: tierID})
	}
	return out
}

func TestMVPFlowRegistrationToResult(t *testing.T) {
	f := newMVPFixture(t)
	const path = "/api/v1/tierlists"

	if code, m := f.do(t, http.MethodPost, path, f.author.ID, mvpBody); code != http.StatusForbidden || m["code"] != "email_not_verified" {
		t.Fatalf("create before confirmation: %d %v", code, m)
	}

	author := f.confirmAuthor(t)
	created := f.create(t, author.ID, mvpBody)
	slug := mvpString(t, created, "slug")
	listID := mvpString(t, created, "id")
	if created["status"] != "open" || created["visibility"] != "public" {
		t.Fatalf("created = %v", created)
	}

	code, shared := f.do(t, http.MethodGet, path+"/"+slug, "", "")
	if code != http.StatusOK {
		t.Fatalf("guest open shared link: %d %v", code, shared)
	}
	if shared["id"] != listID || shared["is_owner"] != false {
		t.Fatalf("shared = %v", shared)
	}
	items := mvpObjects(t, shared, "items")
	tiers := mvpObjects(t, shared, "tiers")
	if len(items) != 3 || len(tiers) != 2 {
		t.Fatalf("items=%d tiers=%d", len(items), len(tiers))
	}

	placements := mvpPlacements(t, items, tiers, 0)
	now := time.Now().UTC()
	const guests = 20
	var firstResultID string
	for i := range guests {
		saved, err := f.store.Save(f.ctx, tierlist.Submission{
			ID:         fmt.Sprintf("guest-result-%02d", i),
			TierlistID: listID,
			SessionID:  fmt.Sprintf("anon-session-%02d", i),
			Placements: placements,
		}, now.Add(time.Duration(i)*time.Second))
		if err != nil {
			t.Fatalf("guest %d save: %v", i, err)
		}
		if saved.Replaced {
			t.Fatalf("guest %d unexpectedly replaced an answer", i)
		}
		if i == 0 {
			firstResultID = saved.SubmissionID
		}
	}

	code, after := f.do(t, http.MethodGet, path+"/"+slug, "", "")
	if code != http.StatusOK || after["submissions_count"] != float64(guests) {
		t.Fatalf("after guests: %d %v", code, after)
	}

	stored, err := f.store.Answers(f.ctx, listID)
	if err != nil || len(stored) != guests {
		t.Fatalf("answers: %v, len=%d", err, len(stored))
	}
	var mine similarity.Answer
	others := make([]similarity.Answer, 0, len(stored)-1)
	for _, answer := range stored {
		if answer.ID == firstResultID {
			mine = answer.Answer
			continue
		}
		others = append(others, answer.Answer)
	}
	if mine == nil {
		t.Fatalf("result %q is missing", firstResultID)
	}
	result := similarity.ThinkLikeYou(mine, others, similarity.DefaultOptions(len(tiers)))
	if result.NotEnoughData || result.Answers != guests || result.Compared != guests-1 || result.Share != 1 {
		t.Fatalf("result = %+v", result)
	}

	distributions, err := f.store.Distributions(f.ctx, listID)
	if err != nil || len(distributions) != len(items) {
		t.Fatalf("distributions: %v, len=%d", err, len(distributions))
	}
	for _, distribution := range distributions {
		if len(distribution.Counts) != len(tiers) || distribution.Counts[0] != guests || distribution.Counts[1] != 0 {
			t.Fatalf("distribution %s = %v", distribution.ItemID, distribution.Counts)
		}
	}
}

func TestMVPResubmissionContract(t *testing.T) {
	f := newMVPFixture(t)
	author := f.confirmAuthor(t)
	created := f.create(t, author.ID, mvpBody)
	listID := mvpString(t, created, "id")

	code, shared := f.do(t, http.MethodGet, "/api/v1/tierlists/"+mvpString(t, created, "slug"), "", "")
	if code != http.StatusOK {
		t.Fatalf("guest get: %d %v", code, shared)
	}
	items := mvpObjects(t, shared, "items")
	tiers := mvpObjects(t, shared, "tiers")

	now := time.Now().UTC()
	first, err := f.store.Save(f.ctx, tierlist.Submission{
		ID: "first-answer", TierlistID: listID, SessionID: "same-anon-session",
		Placements: mvpPlacements(t, items, tiers, 0),
	}, now)
	if err != nil || first.Replaced {
		t.Fatalf("first save: %v %+v", err, first)
	}

	replaced, err := f.store.Save(f.ctx, tierlist.Submission{
		ID: "ignored-new-id", TierlistID: listID, SessionID: "same-anon-session",
		Placements: mvpPlacements(t, items, tiers, 1),
	}, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !replaced.Replaced || replaced.SubmissionID != first.SubmissionID {
		t.Fatalf("resubmission = %+v, first id = %q", replaced, first.SubmissionID)
	}
	if count, err := f.store.CountSubmissions(f.ctx, listID); err != nil || count != 1 {
		t.Fatalf("submissions after resubmission = %d %v, want 1", count, err)
	}
	stored, err := f.store.Answers(f.ctx, listID)
	if err != nil || len(stored) != 1 || stored[0].ID != first.SubmissionID {
		t.Fatalf("stored answers: %v %+v", err, stored)
	}
	for _, item := range items {
		itemID := similarity.ItemID(mvpString(t, item, "id"))
		if got := stored[0].Answer[itemID]; got != 1 {
			t.Fatalf("item %s tier = %d, want 1", itemID, got)
		}
	}

	// Replacement must move the counters too: the old tier loses one and the
	// new tier gains one, keeping exactly one answer worth of placements.
	distributions, err := f.store.Distributions(f.ctx, listID)
	if err != nil || len(distributions) != len(items) {
		t.Fatalf("distributions after resubmission: %v, len=%d", err, len(distributions))
	}
	for _, distribution := range distributions {
		if len(distribution.Counts) != len(tiers) || distribution.Counts[0] != 0 || distribution.Counts[1] != 1 {
			t.Fatalf("distribution %s after resubmission = %v", distribution.ItemID, distribution.Counts)
		}
	}
}

func TestMVPUnlistedNotInCatalog(t *testing.T) {
	f := newMVPFixture(t)
	author := f.confirmAuthor(t)
	const path = "/api/v1/tierlists"

	unlistedBody := strings.Replace(mvpBody, `"visibility":"public"`, `"visibility":"unlisted"`, 1)
	unlisted := f.create(t, author.ID, unlistedBody)
	unlistedID := mvpString(t, unlisted, "id")
	unlistedSlug := mvpString(t, unlisted, "slug")
	if unlistedSlug == "mvp-public" || len(unlistedSlug) < domain.MinUnlistedSlugLength {
		t.Fatalf("unlisted slug = %q", unlistedSlug)
	}
	if code, _ := f.do(t, http.MethodGet, path+"/mvp-public", "", ""); code != http.StatusNotFound {
		t.Fatalf("ignored slug must not exist: %d", code)
	}
	code, shared := f.do(t, http.MethodGet, path+"/"+unlistedSlug, "", "")
	if code != http.StatusOK || shared["id"] != unlistedID || shared["visibility"] != "unlisted" {
		t.Fatalf("unlisted by link: %d %v", code, shared)
	}

	public := f.create(t, author.ID, mvpBody)
	publicID := mvpString(t, public, "id")

	page, err := f.store.ListPublic(f.ctx, tierlist.PageParams{Limit: tierlist.MaxPageSize})
	if err != nil {
		t.Fatal(err)
	}
	var foundPublic, foundUnlisted bool
	for _, entry := range page.Entries {
		switch entry.ID {
		case publicID:
			foundPublic = true
		case unlistedID:
			foundUnlisted = true
		}
	}
	if !foundPublic {
		t.Fatal("public tier list is missing from the catalog")
	}
	if foundUnlisted {
		t.Fatal("unlisted tier list leaked into the catalog")
	}
	if len(page.Entries) != 1 {
		t.Fatalf("catalog has %d entries, want only the public one", len(page.Entries))
	}
}
