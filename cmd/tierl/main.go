// Command tierl is the tier list service entrypoint.
package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"code.sarnas.ru/tierl/internal/app"
	"code.sarnas.ru/tierl/internal/platform/config"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		if err := healthcheck(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "tierl:", err)
			os.Exit(1)
		}
		return
	}
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "tierl:", err)
		os.Exit(1)
	}
}

// healthcheck probes the local HTTP server and exits non-zero when it is not
// ready. Usage: tierl healthcheck [path], path defaults to /readyz.
func healthcheck(args []string) error {
	path := "/readyz"
	if len(args) > 0 {
		path = "/" + strings.TrimPrefix(args[0], "/")
	}
	addr := os.Getenv("TIERL_HTTP_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("healthcheck: bad TIERL_HTTP_ADDR %q: %w", addr, err)
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	url := "http://" + net.JoinHostPort(host, port) + path
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil) //nolint:gosec // URL is built from the process config, not from a request
	if err != nil {
		return fmt.Errorf("healthcheck: %w", err)
	}
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Do(req) //nolint:gosec // URL is built from the process config, not from a request
	if err != nil {
		return fmt.Errorf("healthcheck: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("healthcheck: %s: %s", url, resp.Status)
	}
	return nil
}

func run() error {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", cfg.HTTPAddr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", cfg.HTTPAddr, err)
	}
	return app.Run(ctx, cfg, log, ln)
}
