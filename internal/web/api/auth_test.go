package api_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/basili4-1982/tierl/internal/auth"
	"github.com/basili4-1982/tierl/internal/tierlist"
	"github.com/basili4-1982/tierl/internal/tierlist/tierlisttest"
	"github.com/basili4-1982/tierl/internal/web/api"
)

type authUsers struct {
	mu    sync.Mutex
	byID  map[string]*auth.User
	email map[string]*auth.User
}

func newAuthUsers() *authUsers {
	return &authUsers{byID: map[string]*auth.User{}, email: map[string]*auth.User{}}
}

func (u *authUsers) Create(_ context.Context, user *auth.User) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	if _, ok := u.email[user.Email]; ok {
		return auth.ErrEmailTaken
	}
	c := *user
	u.byID[user.ID] = &c
	u.email[user.Email] = &c
	return nil
}

func (u *authUsers) GetByEmail(_ context.Context, email string) (*auth.User, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if x, ok := u.email[email]; ok {
		c := *x
		return &c, nil
	}
	return nil, auth.ErrUserNotFound
}

func (u *authUsers) GetByID(_ context.Context, id string) (*auth.User, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if x, ok := u.byID[id]; ok {
		c := *x
		return &c, nil
	}
	return nil, auth.ErrUserNotFound
}

func (u *authUsers) UpdatePasswordHash(_ context.Context, id, hash string, now time.Time) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	x, ok := u.byID[id]
	if !ok {
		return auth.ErrUserNotFound
	}
	x.PasswordHash, x.UpdatedAt = hash, now
	if y := u.email[x.Email]; y != nil {
		y.PasswordHash, y.UpdatedAt = hash, now
	}
	return nil
}

func (u *authUsers) MarkEmailVerified(_ context.Context, id string, now time.Time) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	x, ok := u.byID[id]
	if !ok {
		return auth.ErrUserNotFound
	}
	x.EmailVerified, x.UpdatedAt = true, now
	if y := u.email[x.Email]; y != nil {
		y.EmailVerified, y.UpdatedAt = true, now
	}
	return nil
}

type authSessions struct {
	mu   sync.Mutex
	rows map[string]*auth.Session
}

func newAuthSessions() *authSessions { return &authSessions{rows: map[string]*auth.Session{}} }

func (s *authSessions) Create(_ context.Context, sess *auth.Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := *sess
	s.rows[sess.TokenHash] = &c
	return nil
}

func (s *authSessions) GetByTokenHash(_ context.Context, hash string, now time.Time) (*auth.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	x, ok := s.rows[hash]
	if !ok || !x.ExpiresAt.After(now) {
		return nil, auth.ErrSessionNotFound
	}
	c := *x
	return &c, nil
}

func (s *authSessions) Delete(_ context.Context, hash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.rows, hash)
	return nil
}

func (s *authSessions) DeleteByUserID(_ context.Context, userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, x := range s.rows {
		if x.UserID == userID {
			delete(s.rows, k)
		}
	}
	return nil
}

func (s *authSessions) DeleteOthersByUserID(_ context.Context, userID, keepTokenHash string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var n int64
	for k, x := range s.rows {
		if x.UserID == userID && k != keepTokenHash {
			delete(s.rows, k)
			n++
		}
	}
	return n, nil
}

func (s *authSessions) DeleteExpired(_ context.Context, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, x := range s.rows {
		if !x.ExpiresAt.After(now) {
			delete(s.rows, k)
		}
	}
	return nil
}

func (s *authSessions) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.rows)
}

type authEnv struct {
	h        http.Handler
	sessions *authSessions
	csrf     string
}

func newAuthEnv(t *testing.T) authEnv {
	t.Helper()
	users := newAuthUsers()
	sessions := newAuthSessions()
	manager := auth.NewSessionManager(sessions, auth.SessionOptions{})
	svc, err := auth.NewService(users, auth.Options{
		Hash:     auth.HashParams{MemoryKiB: 64, Iterations: 1, Parallelism: 1},
		Sessions: manager,
	})
	if err != nil {
		t.Fatal(err)
	}
	tlSvc := tierlist.NewService(tierlisttest.NewMemStore(), tierlisttest.Users{}, tierlist.ServiceDeps{})
	h := api.New(api.Deps{
		Tierlists:   tlSvc,
		Auth:        headerAuth{},
		AuthService: svc,
		Sessions:    manager,
		Log:         slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	e := authEnv{h: h, sessions: sessions}
	e.csrf = e.getCSRF(t)
	return e
}

func (e authEnv) getCSRF(t *testing.T) string {
	t.Helper()
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, api.Prefix+"/csrf", nil))
	var tok struct {
		CSRF string `json:"csrf_token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &tok); err != nil || tok.CSRF == "" {
		t.Fatalf("GET /csrf: %d %s", rec.Code, rec.Body.String())
	}
	return tok.CSRF
}

func (e authEnv) do(t *testing.T, method, path, body, csrf, session string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, api.Prefix+path, strings.NewReader(body))
	if csrf != "" {
		req.Header.Set(api.CSRFHeader, csrf)
	}
	if session != "" {
		req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: session})
	}
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	return rec
}

func sessionFrom(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == auth.SessionCookieName {
			if !c.HttpOnly {
				t.Error("session cookie is not HttpOnly")
			}
			if c.SameSite != http.SameSiteLaxMode {
				t.Errorf("SameSite = %v, want Lax", c.SameSite)
			}
			if c.Value != "" {
				return c.Value
			}
		}
	}
	return ""
}

func TestAuthHTTPFlow(t *testing.T) {
	e := newAuthEnv(t)

	// Unsafe requests without CSRF are rejected before reaching a handler.
	rec := e.do(t, http.MethodPost, "/auth/register", `{"email":"anna@example.com","password":"correct-horse-battery","display_name":"Anna"}`, "", "")
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "csrf_failed") {
		t.Fatalf("register without CSRF: %d %s", rec.Code, rec.Body.String())
	}

	rec = e.do(t, http.MethodPost, "/auth/register", `{"email":"anna@example.com","password":"correct-horse-battery","display_name":"Anna"}`, e.csrf, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("register: %d %s", rec.Code, rec.Body.String())
	}
	session := sessionFrom(t, rec)
	if session == "" || e.sessions.Len() != 1 {
		t.Fatalf("session not created: cookie=%q rows=%d", session, e.sessions.Len())
	}

	rec = e.do(t, http.MethodGet, "/auth/me", "", "", session)
	if rec.Code != http.StatusOK {
		t.Fatalf("me: %d %s", rec.Code, rec.Body.String())
	}
	var me struct {
		Email string `json:"email"`
		CSRF  string `json:"csrf_token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &me); err != nil || me.Email != "anna@example.com" || me.CSRF == "" {
		t.Fatalf("me body: %s (%v)", rec.Body.String(), err)
	}

	// Login rotates to a new session and works with the token from /auth/me.
	rec = e.do(t, http.MethodPost, "/auth/login", `{"email":"anna@example.com","password":"correct-horse-battery"}`, me.CSRF, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("login: %d %s", rec.Code, rec.Body.String())
	}
	loginSession := sessionFrom(t, rec)
	if loginSession == "" || loginSession == session {
		t.Fatal("login must open a fresh session")
	}

	// Logout deletes only the current session.
	rec = e.do(t, http.MethodPost, "/auth/logout", "", me.CSRF, loginSession)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("logout: %d %s", rec.Code, rec.Body.String())
	}
	if rec = e.do(t, http.MethodGet, "/auth/me", "", "", loginSession); rec.Code != http.StatusUnauthorized {
		t.Fatalf("me after logout: %d", rec.Code)
	}
	if e.sessions.Len() != 1 {
		t.Fatalf("logout removed unrelated sessions: %d", e.sessions.Len())
	}
}

func TestAuthChangePasswordRevokesAllSessions(t *testing.T) {
	e := newAuthEnv(t)
	body := `{"email":"anna@example.com","password":"correct-horse-battery","display_name":"Anna"}`
	rec := e.do(t, http.MethodPost, "/auth/register", body, e.csrf, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("register: %d %s", rec.Code, rec.Body.String())
	}
	first := sessionFrom(t, rec)
	rec = e.do(t, http.MethodPost, "/auth/login", `{"email":"anna@example.com","password":"correct-horse-battery"}`, e.csrf, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("login: %d %s", rec.Code, rec.Body.String())
	}
	second := sessionFrom(t, rec)
	if first == "" || second == "" || e.sessions.Len() != 2 {
		t.Fatalf("sessions: %q %q rows=%d", first, second, e.sessions.Len())
	}

	rec = e.do(t, http.MethodPost, "/auth/change-password",
		`{"current_password":"correct-horse-battery","new_password":"another-long-password"}`, e.csrf, second)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("change password: %d %s", rec.Code, rec.Body.String())
	}
	if e.sessions.Len() != 0 {
		t.Fatalf("password change left %d sessions", e.sessions.Len())
	}
	for _, session := range []string{first, second} {
		if rec := e.do(t, http.MethodGet, "/auth/me", "", "", session); rec.Code != http.StatusUnauthorized {
			t.Fatalf("session survived password change: %d", rec.Code)
		}
	}
	rec = e.do(t, http.MethodPost, "/auth/login", `{"email":"anna@example.com","password":"another-long-password"}`, e.csrf, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("login with new password: %d %s", rec.Code, rec.Body.String())
	}
	if rec = e.do(t, http.MethodPost, "/auth/login", `{"email":"anna@example.com","password":"correct-horse-battery"}`, e.csrf, ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("old password still works: %d", rec.Code)
	}
}

func TestAuthLoginInvalidCredentials(t *testing.T) {
	e := newAuthEnv(t)
	rec := e.do(t, http.MethodPost, "/auth/login", `{"email":"nobody@example.com","password":"whatever-password"}`, e.csrf, "")
	var m map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &m)
	if rec.Code != http.StatusUnauthorized || m["code"] != "invalid_credentials" {
		t.Fatalf("login: %d %v", rec.Code, m)
	}
}

func TestMePasswordKeepsCurrentSession(t *testing.T) {
	e := newAuthEnv(t)
	body := `{"email":"anna@example.com","password":"correct-horse-battery","display_name":"Anna"}`
	rec := e.do(t, http.MethodPost, "/auth/register", body, e.csrf, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("register: %d %s", rec.Code, rec.Body.String())
	}
	first := sessionFrom(t, rec)
	rec = e.do(t, http.MethodPost, "/auth/login", `{"email":"anna@example.com","password":"correct-horse-battery"}`, e.csrf, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("login: %d %s", rec.Code, rec.Body.String())
	}
	current := sessionFrom(t, rec)
	if first == "" || current == "" || e.sessions.Len() != 2 {
		t.Fatalf("sessions: %q %q rows=%d", first, current, e.sessions.Len())
	}

	// The current session must stay usable after the change.
	rec = e.do(t, http.MethodGet, "/auth/me", "", "", current)
	if rec.Code != http.StatusOK {
		t.Fatalf("me before change: %d", rec.Code)
	}

	rec = e.do(t, http.MethodPost, "/me/password",
		`{"current_password":"correct-horse-battery","new_password":"another-long-password"}`, e.csrf, current)
	if rec.Code != http.StatusOK {
		t.Fatalf("change password: %d %s", rec.Code, rec.Body.String())
	}
	var changed struct {
		CSRF    string `json:"csrf_token"`
		Revoked int64  `json:"revoked_sessions"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &changed); err != nil || changed.CSRF == "" {
		t.Fatalf("response: %s (%v)", rec.Body.String(), err)
	}
	if changed.Revoked != 1 {
		t.Errorf("revoked_sessions = %d, want 1", changed.Revoked)
	}
	if e.sessions.Len() != 1 {
		t.Fatalf("expected only the current session, got %d", e.sessions.Len())
	}
	if rec := e.do(t, http.MethodGet, "/auth/me", "", "", current); rec.Code != http.StatusOK {
		t.Fatalf("current session before: %d", rec.Code)
	}
	if rec := e.do(t, http.MethodGet, "/auth/me", "", "", first); rec.Code != http.StatusUnauthorized {
		t.Fatalf("old session survived: %d", rec.Code)
	}
	if rec := e.do(t, http.MethodPost, "/auth/login", `{"email":"anna@example.com","password":"correct-horse-battery"}`, e.csrf, ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("old password still works: %d", rec.Code)
	}
	if rec := e.do(t, http.MethodPost, "/auth/login", `{"email":"anna@example.com","password":"another-long-password"}`, e.csrf, ""); rec.Code != http.StatusOK {
		t.Fatalf("new password rejected: %d %s", rec.Code, rec.Body.String())
	}

	// A weak new password is a 400 with a field reason; sessions stay intact.
	rec = e.do(t, http.MethodPost, "/me/password", `{"current_password":"another-long-password","new_password":"short"}`, e.csrf, current)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("weak password: %d %s", rec.Code, rec.Body.String())
	}
	var m map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &m)
	fields, _ := m["details"].(map[string]any)["fields"].(map[string]any)
	if fields["password"] == nil {
		t.Errorf("weak password has no reason: %v", m)
	}
}
