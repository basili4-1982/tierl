package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestHealthcheck(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if r.URL.Path == "/readyz" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TIERL_HTTP_ADDR", u.Host)

	if err := healthcheck(nil); err != nil {
		t.Fatalf("ready server: %v", err)
	}
	if gotPath != "/readyz" {
		t.Fatalf("probed %q, want /readyz", gotPath)
	}
	if err := healthcheck([]string{"healthz"}); err == nil {
		t.Fatal("503 response must fail the healthcheck")
	}
	if gotPath != "/healthz" {
		t.Fatalf("probed %q, want /healthz", gotPath)
	}
}

func TestHealthcheckBadAddr(t *testing.T) {
	t.Setenv("TIERL_HTTP_ADDR", "nope")
	if err := healthcheck(nil); err == nil || !strings.Contains(err.Error(), "bad TIERL_HTTP_ADDR") {
		t.Fatalf("err = %v, want bad TIERL_HTTP_ADDR", err)
	}
}

func TestHealthcheckUnreachable(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TIERL_HTTP_ADDR", addr)
	if err := healthcheck(nil); err == nil {
		t.Fatal("closed port must fail the healthcheck")
	}
}
