package postgres_test

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"code.sarnas.ru/tierl/internal/storage/postgres"
)

// Integration tests need Docker (Postgres 17 via testcontainers). Without
// Docker they are skipped with a clear message; set TIERL_REQUIRE_DOCKER=1
// (CI does) to fail instead of skipping.
//
// One container is shared by the package; every test builds a fresh schema
// state with truncate so tests stay independent.

var testURL string

func TestMain(m *testing.M) {
	os.Exit(run(m))
}

func run(m *testing.M) int {
	ctx := context.Background()
	if os.Getenv("TIERL_TEST_DATABASE_URL") != "" {
		testURL = os.Getenv("TIERL_TEST_DATABASE_URL")
		return m.Run()
	}
	ctr, err := startPostgres(ctx)
	if err != nil {
		if os.Getenv("TIERL_REQUIRE_DOCKER") != "" {
			println("postgres integration tests: Docker is required but unavailable:", err.Error())
			return 1
		}
		println("postgres integration tests SKIPPED: Docker is unavailable (" + err.Error() + "); set TIERL_REQUIRE_DOCKER=1 to fail instead")
		return 0
	}
	code := m.Run()
	_ = testcontainers.TerminateContainer(ctr)
	return code
}

func startPostgres(ctx context.Context) (ctr *tcpostgres.PostgresContainer, err error) {
	defer func() {
		// testcontainers panics when no Docker provider can be created.
		if r := recover(); r != nil {
			err = errPanic{r}
		}
	}()
	ctr, err = tcpostgres.Run(ctx, "postgres:17-alpine",
		tcpostgres.WithDatabase("tierl"), tcpostgres.WithUsername("tierl"), tcpostgres.WithPassword("tierl"),
		tcpostgres.BasicWaitStrategies(),
		testcontainers.WithWaitStrategy(wait.ForListeningPort("5432/tcp")),
	)
	if err != nil {
		return nil, err
	}
	testURL, err = ctr.ConnectionString(ctx, "sslmode=disable")
	return ctr, err
}

type errPanic struct{ v any }

func (e errPanic) Error() string { return "panic: " + toString(e.v) }

func toString(v any) string {
	if e, ok := v.(error); ok {
		return e.Error()
	}
	if s, ok := v.(string); ok {
		return s
	}
	return "unknown"
}

// newStore migrates a clean schema and returns a store and its pool.
func newStore(t *testing.T) (*postgres.Store, *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	if err := postgres.MigrateDown(testURL); err != nil {
		t.Fatalf("migrate down: %v", err)
	}
	if err := postgres.MigrateUp(testURL); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	pool, err := postgres.Open(ctx, testURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return postgres.New(pool), pool
}
