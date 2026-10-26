package app

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/basili4-1982/tierl/internal/platform/config"
	"github.com/basili4-1982/tierl/internal/storage/postgres"
	"github.com/basili4-1982/tierl/internal/tierlist"
	"github.com/basili4-1982/tierl/internal/tierlist/tierlisttest"
	"github.com/basili4-1982/tierl/internal/web"
)

// Integration tests need Docker (Postgres 17 via testcontainers). Without
// Docker they are skipped; set TIERL_REQUIRE_DOCKER=1 (CI does) to fail
// instead. TIERL_TEST_DATABASE_URL points them at an existing database.

var (
	dbOnce sync.Once
	dbURL  string
	dbErr  error
	dbCtr  *tcpostgres.PostgresContainer
)

func TestMain(m *testing.M) {
	code := m.Run()
	if dbCtr != nil {
		_ = testcontainers.TerminateContainer(dbCtr)
	}
	os.Exit(code)
}

func testDB(t *testing.T) string {
	t.Helper()
	dbOnce.Do(func() {
		if u := os.Getenv("TIERL_TEST_DATABASE_URL"); u != "" {
			dbURL = u
			return
		}
		dbURL, dbErr = startPostgres(context.Background())
	})
	if dbErr != nil {
		if os.Getenv("TIERL_REQUIRE_DOCKER") != "" {
			t.Fatalf("Docker is required but unavailable: %v", dbErr)
		}
		t.Skipf("Docker is unavailable (%v); set TIERL_REQUIRE_DOCKER=1 to fail instead", dbErr)
	}
	// Every test starts from an empty schema.
	if err := postgres.MigrateDown(dbURL); err != nil {
		t.Fatalf("migrate down: %v", err)
	}
	if err := postgres.MigrateUp(dbURL); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	return dbURL
}

func startPostgres(ctx context.Context) (url string, err error) {
	defer func() {
		// testcontainers panics when no Docker provider can be created.
		if r := recover(); r != nil {
			err = &panicError{r}
		}
	}()
	dbCtr, err = tcpostgres.Run(ctx, "postgres:17-alpine",
		tcpostgres.WithDatabase("tierl"), tcpostgres.WithUsername("tierl"), tcpostgres.WithPassword("tierl"),
		tcpostgres.BasicWaitStrategies(),
		testcontainers.WithWaitStrategy(wait.ForListeningPort("5432/tcp")),
	)
	if err != nil {
		return "", err
	}
	return dbCtr.ConnectionString(ctx, "sslmode=disable")
}

type panicError struct{ v any }

func (e *panicError) Error() string {
	if err, ok := e.v.(error); ok {
		return "panic: " + err.Error()
	}
	return "panic"
}

func discardLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func testConfig(url string) config.Config {
	return config.Config{
		DatabaseURL:            url,
		IPHashSalt:             "test-salt",
		CreateTierlistBurst:    3,
		CreateTierlistInterval: time.Hour,
		ShutdownTimeout:        time.Second,
	}
}

// headerAuth identifies the user by the X-Test-User header.
type headerAuth struct{}

func (headerAuth) UserID(r *http.Request) (string, bool) {
	id := r.Header.Get("X-Test-User")
	return id, id != ""
}

const body = `{"title":"Languages","description":"d","visibility":"public","slug":"langs",
"tiers":[{"name":"S","color":"#ff0000"},{"name":"A","color":"#00ff00"}],
"items":[{"label":"Go"},{"label":"Rust"},{"label":"Zig"}]}`

func call(t *testing.T, h http.Handler, method, path, user, payload string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(payload))
	req.RemoteAddr = "203.0.113.7:4000"
	if user != "" {
		req.Header.Set("X-Test-User", user)
	}
	if method != http.MethodGet && method != http.MethodHead {
		req.Header.Set("X-CSRF-Token", csrfToken(t, h))
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var m map[string]any
	if rec.Body.Len() > 0 {
		_ = json.Unmarshal(rec.Body.Bytes(), &m)
	}
	return rec.Code, m
}

func csrfToken(t *testing.T, h http.Handler) string {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/csrf", nil))
	var tok struct {
		CSRF string `json:"csrf_token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &tok); err != nil || tok.CSRF == "" {
		t.Fatalf("GET /csrf: %d %s (%v)", rec.Code, rec.Body.String(), err)
	}
	return tok.CSRF
}

func TestAPIFlowOnRealDatabase(t *testing.T) {
	url := testDB(t)
	ctx := context.Background()
	pool, err := postgres.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	users := tierlisttest.Users{
		"anna": {DisplayName: "Anna", EmailVerified: true},
		"bob":  {DisplayName: "Bob", EmailVerified: true},
	}
	deps, err := newDeps(pool, testConfig(url), discardLog(), headerAuth{}, users)
	if err != nil {
		t.Fatal(err)
	}
	h := web.NewHandler(deps)
	const p = "/api/v1/tierlists"

	if code, m := call(t, h, http.MethodPost, p, "", body); code != 401 || m["code"] != "unauthorized" {
		t.Fatalf("guest create: %d %v", code, m)
	}
	code, created := call(t, h, http.MethodPost, p, "anna", body)
	if code != 201 || created["slug"] != "langs" || created["status"] != "open" {
		t.Fatalf("create: %d %v", code, created)
	}
	if code, m := call(t, h, http.MethodPost, p, "bob", body); code != 409 || m["code"] != "slug_taken" {
		t.Fatalf("duplicate slug: %d %v", code, m)
	}

	code, got := call(t, h, http.MethodGet, p+"/langs", "bob", "")
	if code != 200 || got["is_owner"] != false || got["id"] != created["id"] {
		t.Fatalf("get: %d %v", code, got)
	}
	if items, _ := got["items"].([]any); len(items) != 3 {
		t.Fatalf("items = %v", got["items"])
	}

	if code, m := call(t, h, http.MethodPatch, p+"/langs", "bob", `{"title":"hack"}`); code != 403 || m["code"] != "forbidden" {
		t.Fatalf("patch by stranger: %d %v", code, m)
	}
	if code, m := call(t, h, http.MethodPatch, p+"/langs", "anna", `{"title":"Renamed"}`); code != 200 || m["title"] != "Renamed" {
		t.Fatalf("patch: %d %v", code, m)
	}
	structure := `{"items":[{"label":"A"},{"label":"B"},{"label":"C"},{"label":"D"}]}`
	if code, m := call(t, h, http.MethodPatch, p+"/langs", "anna", structure); code != 200 || len(m["items"].([]any)) != 4 {
		t.Fatalf("structure patch before answers: %d %v", code, m)
	}

	// The first answer locks the structure .
	lockStructure(t, postgres.New(pool), "langs")
	if code, m := call(t, h, http.MethodPatch, p+"/langs", "anna", `{"items":[{"label":"x"},{"label":"y"},{"label":"z"}]}`); code != 409 || m["code"] != "tierlist_locked" {
		t.Fatalf("patch after answer: %d %v", code, m)
	}
	if code, m := call(t, h, http.MethodPatch, p+"/langs", "anna", `{"description":"still ok"}`); code != 200 || m["submissions_count"] != float64(1) {
		t.Fatalf("text patch after answer: %d %v", code, m)
	}

	if code, m := call(t, h, http.MethodPost, p+"/langs/close", "anna", ""); code != 200 || m["status"] != "closed" || m["closed_at"] == nil {
		t.Fatalf("close: %d %v", code, m)
	}
	if code, m := call(t, h, http.MethodPost, p+"/langs/close", "anna", ""); code != 200 || m["status"] != "closed" {
		t.Fatalf("close again: %d %v", code, m)
	}

	if code, _ := call(t, h, http.MethodDelete, p+"/langs", "bob", ""); code != 403 {
		t.Fatalf("delete by stranger: %d", code)
	}
	if code, _ := call(t, h, http.MethodDelete, p+"/langs", "anna", ""); code != 204 {
		t.Fatalf("delete: %d", code)
	}
	if code, _ := call(t, h, http.MethodGet, p+"/langs", "", ""); code != 404 {
		t.Fatalf("get deleted: %d", code)
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM submissions`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("answers must be deleted with the tier list: %d %v", n, err)
	}

	// Creation is rate limited per IP: the three POSTs above used the burst of 3.
	code, m := call(t, h, http.MethodPost, p, "anna", body)
	if code != 429 || m["code"] != "rate_limited" {
		t.Fatalf("expected rate limit, got %d %v", code, m)
	}
}

// lockStructure stores the first answer the way the answers module will.
func lockStructure(t *testing.T, store *postgres.Store, slug string) {
	t.Helper()
	ctx := context.Background()
	tl, err := store.GetBySlug(ctx, slug)
	if err != nil {
		t.Fatal(err)
	}
	var pl []tierlist.Placement
	for _, it := range tl.Items[:3] {
		pl = append(pl, tierlist.Placement{ItemID: it.ID, TierID: tl.Tiers[0].ID})
	}
	_, err = store.Save(ctx, tierlist.Submission{ID: "sub-1", TierlistID: tl.ID, SessionID: "s1", Placements: pl}, time.Now())
	if err != nil {
		t.Fatalf("save answer: %v", err)
	}
}

func TestSetup_RealDatabase(t *testing.T) {
	url := testDB(t)
	cfg := testConfig(url)
	cfg.MigrateOnStart = true

	deps, closeFn, err := setup(context.Background(), cfg, discardLog())
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	h := web.NewHandler(deps)

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("readyz = %d", rec.Code)
	}
	if code, m := call(t, h, http.MethodPost, "/api/v1/tierlists", "", body); code != 401 || m["code"] != "unauthorized" {
		t.Fatalf("placeholder auth must reject creation: %d %v", code, m)
	}
	if code, _ := call(t, h, http.MethodGet, "/api/v1/tierlists/none", "", ""); code != 404 {
		t.Fatalf("get missing = %d", code)
	}

	closeFn()
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("readyz after close = %d, want 503", rec.Code)
	}
}

func TestRun_WithDatabaseGracefulClose(t *testing.T) {
	url := testDB(t)
	cfg := testConfig(url)
	cfg.MigrateOnStart = true

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- Run(ctx, cfg, discardLog(), ln) }()

	for _, path := range []string{"/healthz", "/readyz"} {
		resp, err := http.Get("http://" + ln.Addr().String() + path)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s = %d", path, resp.StatusCode)
		}
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not stop")
	}
}

func TestSetup_Errors(t *testing.T) {
	t.Run("no database disables the API", func(t *testing.T) {
		deps, closeFn, err := setup(context.Background(), config.Config{}, discardLog())
		if err != nil || deps.API != nil || deps.Ready != nil {
			t.Fatalf("got %+v, %v", deps, err)
		}
		closeFn()
	})
	t.Run("unreachable database", func(t *testing.T) {
		cfg := testConfig("postgres://tierl:tierl@127.0.0.1:1/tierl?sslmode=disable")
		if _, _, err := setup(context.Background(), cfg, discardLog()); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("migration failure", func(t *testing.T) {
		cfg := testConfig("postgres://tierl:tierl@127.0.0.1:1/tierl?sslmode=disable")
		cfg.MigrateOnStart = true
		if _, _, err := setup(context.Background(), cfg, discardLog()); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("invalid rate limit config", func(t *testing.T) {
		url := testDB(t)
		cfg := testConfig(url)
		cfg.CreateTierlistBurst = 0
		if _, _, err := setup(context.Background(), cfg, discardLog()); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestPlaceholders(t *testing.T) {
	if id, ok := (guestAuth{}).UserID(httptest.NewRequest(http.MethodGet, "/", nil)); ok || id != "" {
		t.Errorf("guestAuth = %q, %v", id, ok)
	}
	if _, err := (noUsers{}).Profile(context.Background(), "x"); err == nil {
		t.Error("noUsers must not know anybody")
	}
	_ = tierlist.ErrNotFound
}
