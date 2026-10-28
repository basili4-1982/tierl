package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"code.sarnas.ru/tierl/internal/auth"
	"code.sarnas.ru/tierl/internal/domain"
	"code.sarnas.ru/tierl/internal/platform/mailer/mailertest"
	"code.sarnas.ru/tierl/internal/storage/postgres"
	"code.sarnas.ru/tierl/internal/tierlist"
	"code.sarnas.ru/tierl/internal/web"
	"code.sarnas.ru/tierl/internal/web/api"
)

const (
	mvpPassword     = "correct-horse-battery"
	mvpRegisterBody = `{"email":"Anna@Example.COM","password":"correct-horse-battery","display_name":"Anna"}`
	mvpLoginBody    = `{"email":"anna@example.com","password":"correct-horse-battery"}`
	mvpBody         = `{"title":"MVP languages","description":"rank them","visibility":"public","slug":"mvp-public",
"tiers":[{"name":"S","color":"#ff0000"},{"name":"A","color":"#00ff00"}],
"items":[{"label":"Go"},{"label":"Rust"},{"label":"Zig"}]}`
)

var mvpTokenPattern = regexp.MustCompile(`[?&]token=([A-Za-z0-9_-]+)`)

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
	ctx      context.Context
	pool     *pgxpool.Pool
	store    *postgres.Store
	handler  http.Handler
	mail     *mailertest.Capture
	csrf     string
	authorID string
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
	sessions := auth.NewSessionManager(postgres.NewSessionStore(pool), auth.SessionOptions{})
	capture := &mailertest.Capture{}
	authSvc, err := auth.NewService(userStore, auth.Options{
		Hash:     auth.HashParams{MemoryKiB: 64, Iterations: 1, Parallelism: 1},
		Sessions: sessions,
		Tokens:   postgres.NewTokenStore(pool),
		Mail:     capture,
		BaseURL:  "http://localhost:8080",
		Log:      discardLog(),
	})
	if err != nil {
		t.Fatal(err)
	}

	cfg := testConfig(url)
	cfg.CreateTierlistBurst = 10
	bundle := &authBundle{
		service: authSvc, sessions: sessions,
		users: authUsers{users: userStore}, closeMail: func() {},
	}
	deps, err := buildDeps(pool, cfg, discardLog(), headerAuth{}, mvpUsers{store: userStore}, bundle)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeMedia(deps) })

	f := &mvpFixture{
		ctx: ctx, pool: pool, store: postgres.New(pool),
		handler: web.NewHandler(deps), mail: capture,
	}
	f.csrf = csrfToken(t, f.handler)
	code, registered := f.do(t, http.MethodPost, "/api/v1/auth/register", "", mvpRegisterBody)
	if code != http.StatusCreated {
		t.Fatalf("register: %d %v", code, registered)
	}
	f.authorID = mvpString(t, registered, "id")
	return f
}

// confirmAuthor follows the public email confirmation flow: it takes the
// one-time token from the test mail delivery channel, verifies the address
// over HTTP and signs in through the API.
func (f *mvpFixture) confirmAuthor(t *testing.T) string {
	t.Helper()
	token := f.verificationToken(t)
	code, body := f.do(t, http.MethodPost, "/api/v1/auth/verify-email", "",
		mvpJSON(t, map[string]string{"token": token}))
	if code != http.StatusNoContent {
		t.Fatalf("confirm email: %d %v", code, body)
	}
	code, user := f.do(t, http.MethodPost, "/api/v1/auth/login", "", mvpLoginBody)
	if code != http.StatusOK {
		t.Fatalf("login: %d %v", code, user)
	}
	if user["email_verified"] != true {
		t.Fatal("login returned an unverified user")
	}
	id := mvpString(t, user, "id")
	if id != f.authorID {
		t.Fatalf("login returned user %q, want %q", id, f.authorID)
	}
	return id
}

func (f *mvpFixture) verificationToken(t *testing.T) string {
	t.Helper()
	msg, ok := f.mail.Last()
	if !ok {
		t.Fatal("registration did not deliver a verification email")
	}
	for _, body := range []string{msg.Text, msg.HTML} {
		if match := mvpTokenPattern.FindStringSubmatch(body); len(match) == 2 {
			return match[1]
		}
	}
	t.Fatal("verification email has no token link")
	return ""
}

func (f *mvpFixture) do(t *testing.T, method, path, user, payload string) (int, map[string]any) {
	t.Helper()
	code, body, _ := f.doCookie(t, method, path, user, payload, nil)
	return code, body
}

func (f *mvpFixture) doCookie(t *testing.T, method, path, user, payload string, cookie *http.Cookie) (int, map[string]any, []*http.Cookie) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(payload))
	req.RemoteAddr = "203.0.113.7:4000"
	if user != "" {
		req.Header.Set("X-Test-User", user)
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	if method != http.MethodGet && method != http.MethodHead {
		req.Header.Set(api.CSRFHeader, f.csrf)
	}
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)
	var m map[string]any
	if rec.Body.Len() > 0 {
		_ = json.Unmarshal(rec.Body.Bytes(), &m)
	}
	return rec.Code, m, rec.Result().Cookies()
}

func (f *mvpFixture) create(t *testing.T, userID, payload string) map[string]any {
	t.Helper()
	code, created := f.do(t, http.MethodPost, "/api/v1/tierlists", userID, payload)
	if code != http.StatusCreated {
		t.Fatalf("create: %d %v", code, created)
	}
	return created
}

func mvpJSON(t *testing.T, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(data)
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

func mvpSubmissionBody(t *testing.T, placements []tierlist.Placement) string {
	t.Helper()
	raw := make([]map[string]string, len(placements))
	for i, placement := range placements {
		raw[i] = map[string]string{"item_id": placement.ItemID, "tier_id": placement.TierID}
	}
	return mvpJSON(t, map[string]any{"placements": raw})
}

func mvpAnonCookie(t *testing.T, cookies []*http.Cookie) *http.Cookie {
	t.Helper()
	for _, cookie := range cookies {
		if cookie.Name == api.AnonCookieName {
			return cookie
		}
	}
	t.Fatalf("response has no %s cookie", api.AnonCookieName)
	return nil
}

func mvpAssertTier(t *testing.T, submission map[string]any, tierID string) {
	t.Helper()
	placements := mvpObjects(t, submission, "placements")
	if len(placements) == 0 {
		t.Fatal("submission has no placements")
	}
	for _, placement := range placements {
		if got := mvpString(t, placement, "tier_id"); got != tierID {
			t.Fatalf("placement tier = %q, want %q", got, tierID)
		}
	}
}

func mvpAssertDistribution(t *testing.T, stats map[string]any, itemCount int, want map[string]int) {
	t.Helper()
	items := mvpObjects(t, stats, "items")
	if len(items) != itemCount {
		t.Fatalf("stats has %d items, want %d: %v", len(items), itemCount, stats)
	}
	for _, item := range items {
		distribution := mvpObjects(t, item, "distribution")
		if len(distribution) != len(want) {
			t.Fatalf("item %s has %d tiers, want %d", mvpString(t, item, "item_id"), len(distribution), len(want))
		}
		got := make(map[string]int, len(distribution))
		for _, tier := range distribution {
			count, ok := tier["count"].(float64)
			if !ok {
				t.Fatalf("tier %s count = %v", mvpString(t, tier, "tier_id"), tier["count"])
			}
			got[mvpString(t, tier, "tier_id")] = int(count)
		}
		for tierID, count := range want {
			if got[tierID] != count {
				t.Fatalf("item %s tier %s count = %d, want %d", mvpString(t, item, "item_id"), tierID, got[tierID], count)
			}
		}
	}
}

func TestMVPFlowRegistrationToResult(t *testing.T) {
	f := newMVPFixture(t)
	const path = "/api/v1/tierlists"

	if code, m := f.do(t, http.MethodPost, path, f.authorID, mvpBody); code != http.StatusForbidden || m["code"] != "email_not_verified" {
		t.Fatalf("create before confirmation: %d %v", code, m)
	}

	authorID := f.confirmAuthor(t)
	created := f.create(t, authorID, mvpBody)
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
	const guests = 20
	var firstResultID string
	for i := range guests {
		code, saved, cookies := f.doCookie(t, http.MethodPost, path+"/"+slug+"/submissions", "",
			mvpSubmissionBody(t, placements), nil)
		if code != http.StatusCreated {
			t.Fatalf("guest %d submit: %d %v", i, code, saved)
		}
		mvpAnonCookie(t, cookies)
		if i == 0 {
			firstResultID = mvpString(t, saved, "id")
		}
	}

	code, after := f.do(t, http.MethodGet, path+"/"+slug, "", "")
	if code != http.StatusOK || after["submissions_count"] != float64(guests) {
		t.Fatalf("after guests: %d %v", code, after)
	}

	code, result := f.do(t, http.MethodGet, "/api/v1/results/"+firstResultID, "", "")
	if code != http.StatusOK {
		t.Fatalf("result: %d %v", code, result)
	}
	submission := result["submission"].(map[string]any)
	if submission["id"] != firstResultID || submission["tierlist_slug"] != slug {
		t.Fatalf("result submission = %v", submission)
	}
	mvpAssertTier(t, submission, mvpString(t, tiers[0], "id"))
	if tierlistDTO := result["tierlist"].(map[string]any); tierlistDTO["submissions_count"] != float64(guests) {
		t.Fatalf("result tierlist = %v", tierlistDTO)
	}

	code, stats := f.do(t, http.MethodGet, "/api/v1/results/"+firstResultID+"/stats", "", "")
	if code != http.StatusOK {
		t.Fatalf("result stats: %d %v", code, stats)
	}
	if stats["total_submissions"] != float64(guests) || stats["insufficient_data"] != false ||
		stats["like_you_percent"] != float64(100) {
		t.Fatalf("result stats = %v", stats)
	}
	mvpAssertDistribution(t, stats, len(items), map[string]int{
		mvpString(t, tiers[0], "id"): guests,
		mvpString(t, tiers[1], "id"): 0,
	})
}

func TestMVPResubmissionContract(t *testing.T) {
	f := newMVPFixture(t)
	authorID := f.confirmAuthor(t)
	created := f.create(t, authorID, mvpBody)
	listID := mvpString(t, created, "id")
	slug := mvpString(t, created, "slug")

	code, shared := f.do(t, http.MethodGet, "/api/v1/tierlists/"+slug, "", "")
	if code != http.StatusOK {
		t.Fatalf("guest get: %d %v", code, shared)
	}
	items := mvpObjects(t, shared, "items")
	tiers := mvpObjects(t, shared, "tiers")
	firstTierID := mvpString(t, tiers[0], "id")
	secondTierID := mvpString(t, tiers[1], "id")

	code, first, cookies := f.doCookie(t, http.MethodPost, "/api/v1/tierlists/"+slug+"/submissions", "",
		mvpSubmissionBody(t, mvpPlacements(t, items, tiers, 0)), nil)
	if code != http.StatusCreated {
		t.Fatalf("first submit: %d %v", code, first)
	}
	cookie := mvpAnonCookie(t, cookies)
	firstID := mvpString(t, first, "id")

	code, conflict, _ := f.doCookie(t, http.MethodPost, "/api/v1/tierlists/"+slug+"/submissions", "",
		mvpSubmissionBody(t, mvpPlacements(t, items, tiers, 1)), cookie)
	if code != http.StatusConflict || conflict["code"] != "already_submitted" {
		t.Fatalf("repeat submit: %d %v", code, conflict)
	}
	details, _ := conflict["details"].(map[string]any)
	if details["result_id"] != firstID {
		t.Fatalf("repeat submit details = %v, want result_id %q", conflict["details"], firstID)
	}

	code, replaced, _ := f.doCookie(t, http.MethodPut, "/api/v1/tierlists/"+slug+"/submissions/mine", "",
		mvpSubmissionBody(t, mvpPlacements(t, items, tiers, 1)), cookie)
	if code != http.StatusOK {
		t.Fatalf("replace submit: %d %v", code, replaced)
	}
	if replaced["id"] != firstID {
		t.Fatalf("replace changed result id: %v, want %q", replaced["id"], firstID)
	}
	mvpAssertTier(t, replaced, secondTierID)

	code, after := f.do(t, http.MethodGet, "/api/v1/tierlists/"+slug, "", "")
	if code != http.StatusOK || after["submissions_count"] != float64(1) {
		t.Fatalf("after resubmission: %d %v", code, after)
	}

	code, result := f.do(t, http.MethodGet, "/api/v1/results/"+firstID, "", "")
	if code != http.StatusOK {
		t.Fatalf("result: %d %v", code, result)
	}
	if result["submission"].(map[string]any)["id"] != firstID {
		t.Fatalf("result = %v", result)
	}
	if result["tierlist"].(map[string]any)["id"] != listID {
		t.Fatalf("result tierlist = %v", result["tierlist"])
	}

	code, stats := f.do(t, http.MethodGet, "/api/v1/results/"+firstID+"/stats", "", "")
	if code != http.StatusOK {
		t.Fatalf("result stats: %d %v", code, stats)
	}
	if stats["total_submissions"] != float64(1) {
		t.Fatalf("resubmission changed the submission count: %v", stats)
	}
	mvpAssertDistribution(t, stats, len(items), map[string]int{firstTierID: 0, secondTierID: 1})
}

func TestMVPUnlistedNotInCatalog(t *testing.T) {
	f := newMVPFixture(t)
	authorID := f.confirmAuthor(t)
	const path = "/api/v1/tierlists"

	unlistedBody := strings.Replace(mvpBody, `"visibility":"public"`, `"visibility":"unlisted"`, 1)
	unlisted := f.create(t, authorID, unlistedBody)
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

	public := f.create(t, authorID, mvpBody)
	publicID := mvpString(t, public, "id")

	// TEMPORARY(TIERL-33): GET /api/v1/catalog arrives with the add-catalog
	// change. Until then this storage-level check is the only way to verify
	// that unlisted tier lists stay out of the catalog; replace it with an
	// HTTP request once the route exists.
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
