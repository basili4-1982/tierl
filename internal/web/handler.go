// Package web wires the HTTP layer of the service.
package web

import (
	"context"
	"net/http"
	"time"

	"code.sarnas.ru/tierl/internal/web/api"
	"code.sarnas.ru/tierl/internal/web/static"
)

const readyTimeout = 2 * time.Second

// Deps are the optional parts of the root handler.
type Deps struct {
	// API serves the JSON API under api.Prefix; nil leaves the prefix to the SPA fallback.
	API http.Handler
	// Ready checks external dependencies (the database) for /readyz; nil means always ready.
	Ready func(ctx context.Context) error
}

// NewHandler returns the root HTTP handler.
func NewHandler(d Deps) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthz)
	mux.HandleFunc("GET /readyz", readyz(d.Ready))
	if d.API != nil {
		mux.Handle(api.Prefix+"/", d.API)
		if share, ok := d.API.(interface{ ShareHandler() http.Handler }); ok {
			h := share.ShareHandler()
			mux.Handle("/t/", h)
			mux.Handle("/r/", h)
			mux.Handle("/og/", h)
		}
		if store, ok := d.API.(interface {
			MediaHandler() (string, http.Handler)
		}); ok {
			if path, h := store.MediaHandler(); path != "" && path != "/" && h != nil {
				mux.Handle(path, h)
			}
		}
	}
	mux.Handle("/", static.Handler(static.FS()))
	return mux
}

// healthz is the liveness probe: the process serves requests.
func healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok"))
}

// readyz is the readiness probe: dependencies answer.
func readyz(check func(ctx context.Context) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if check != nil {
			ctx, cancel := context.WithTimeout(r.Context(), readyTimeout)
			defer cancel()
			if err := check(ctx); err != nil {
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte("not ready"))
				return
			}
		}
		_, _ = w.Write([]byte("ok"))
	}
}
