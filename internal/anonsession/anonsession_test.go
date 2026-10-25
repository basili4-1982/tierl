package anonsession_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/basili4-1982/tierl/internal/anonsession"
)

func TestNewAndHash(t *testing.T) {
	s, err := anonsession.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	if !s.Fresh || len(s.Token) != 43 || len(s.Hash) != 64 {
		t.Fatalf("session: %+v", s)
	}
	if s.Hash != anonsession.HashToken(s.Token) {
		t.Error("hash does not match the token")
	}
	other, _ := anonsession.New(nil)
	if s.Token == other.Token {
		t.Error("tokens must be random")
	}
}

func TestFromRequest(t *testing.T) {
	s, err := anonsession.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	if _, ok := anonsession.FromRequest(r); ok {
		t.Error("missing cookie must not yield a session")
	}
	r.Header.Set("Cookie", anonsession.CookieName+"="+s.Token)
	got, ok := anonsession.FromRequest(r)
	if !ok || got.Hash != s.Hash || got.Fresh {
		t.Fatalf("from request: %+v %v", got, ok)
	}
	for _, bad := range []string{"", "short", strings.Repeat("!", 43)} {
		r.Header.Set("Cookie", anonsession.CookieName+"="+bad)
		if _, ok := anonsession.FromRequest(r); ok {
			t.Errorf("token %q must be rejected", bad)
		}
	}
}

func TestEnsureAndCookie(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	got, err := anonsession.Ensure(r, nil)
	if err != nil || !got.Fresh {
		t.Fatalf("ensure: %v %+v", err, got)
	}
	rec := httptest.NewRecorder()
	anonsession.SetCookie(rec, got)
	c := rec.Result().Cookies()[0]
	if c.Name != anonsession.CookieName || c.Value != got.Token || c.Path != "/" ||
		!c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteLaxMode {
		t.Fatalf("cookie: %+v", c)
	}

	r.Header.Set("Cookie", anonsession.CookieName+"="+got.Token)
	again, err := anonsession.Ensure(r, nil)
	if err != nil || again.Fresh || again.Token != got.Token {
		t.Fatalf("ensure existing: %v %+v", err, again)
	}
}
