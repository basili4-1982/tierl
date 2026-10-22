// Command migrate applies or reverts database migrations.
//
// Usage: migrate up|down. The database is taken from TIERL_DATABASE_URL.
package main

import (
	"fmt"
	"os"

	"github.com/basili4-1982/tierl/internal/storage/postgres"
)

func main() {
	if err := run(os.Args[1:], os.Getenv); err != nil {
		fmt.Fprintln(os.Stderr, "migrate:", err)
		os.Exit(1)
	}
}

func run(args []string, getenv func(string) string) error {
	if len(args) != 1 || (args[0] != "up" && args[0] != "down") {
		return fmt.Errorf("usage: migrate up|down")
	}
	url := getenv("TIERL_DATABASE_URL")
	if url == "" {
		return fmt.Errorf("TIERL_DATABASE_URL is not set")
	}
	if args[0] == "up" {
		return postgres.MigrateUp(url)
	}
	return postgres.MigrateDown(url)
}
