package postgres

import (
	"errors"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	// Registers the pgx v5 database driver under the "pgx5" scheme.
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"

	"github.com/basili4-1982/tierl/migrations"
)

// newMigrate builds a migrator for the embedded migrations. url is a regular
// postgres:// or postgresql:// URL.
func newMigrate(url string) (*migrate.Migrate, error) {
	src, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return nil, fmt.Errorf("postgres: migrations source: %w", err)
	}
	m, err := migrate.NewWithSourceInstance("iofs", src, toPgx5URL(url))
	if err != nil {
		return nil, fmt.Errorf("postgres: migrator: %w", err)
	}
	return m, nil
}

// toPgx5URL rewrites the scheme to the one the pgx v5 migrate driver registers.
func toPgx5URL(url string) string {
	for _, p := range []string{"postgresql://", "postgres://"} {
		if len(url) > len(p) && url[:len(p)] == p {
			return "pgx5://" + url[len(p):]
		}
	}
	return url
}

// MigrateUp applies all pending migrations. It is a no-op when the schema is current.
func MigrateUp(url string) error {
	return runMigrate(url, (*migrate.Migrate).Up)
}

// MigrateDown reverts every applied migration.
func MigrateDown(url string) error {
	return runMigrate(url, (*migrate.Migrate).Down)
}

func runMigrate(url string, op func(*migrate.Migrate) error) error {
	m, err := newMigrate(url)
	if err != nil {
		return err
	}
	opErr := op(m)
	srcErr, dbErr := m.Close()
	if opErr != nil && !errors.Is(opErr, migrate.ErrNoChange) {
		return fmt.Errorf("postgres: migrate: %w", opErr)
	}
	if err := errors.Join(srcErr, dbErr); err != nil {
		return fmt.Errorf("postgres: close migrator: %w", err)
	}
	return nil
}

// MigrateOnStart applies pending migrations when enabled
// (config TIERL_MIGRATE_ON_START); otherwise it does nothing.
func MigrateOnStart(enabled bool, url string) error {
	if !enabled {
		return nil
	}
	if url == "" {
		return errors.New("postgres: TIERL_MIGRATE_ON_START is set but TIERL_DATABASE_URL is empty")
	}
	return MigrateUp(url)
}
