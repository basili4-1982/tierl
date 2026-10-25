package api_test

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/basili4-1982/tierl/internal/auth"
	"github.com/basili4-1982/tierl/internal/auth/authtest"
	"github.com/basili4-1982/tierl/internal/platform/ratelimit"
	"github.com/basili4-1982/tierl/internal/tierlist"
	"github.com/basili4-1982/tierl/internal/tierlist/tierlisttest"
	"github.com/basili4-1982/tierl/internal/web/api"
)

var fastHash = auth.HashParams{MemoryKiB: 64, Iterations: 1, Parallelism: 1}

// clock is a settable time source shared by the services and the limiter.
type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

type authEnv struct {
	h        http.Handler
	users    *authtest.Users
	sessions *authtest.Sessions
	resets   *authtest.Resets
	mail     *authtest.Mailer
	clock    *clock
}

type authEnvOptions struct {
	burst    int
	secure   bool
	ttl      time.Duration
	maxAge   time.Duration
	resetTTL time.Duration
	// noMail disables the mailer, as in production before SMTP (TIERL-2).
	noMail bool
}

func newAuthEnv(t *testing.T, opts authEnvOptions) *authEnv {
	t.Helper()
	if opts.burst == 0 {
		opts.burst = 1000
	}
	if opts.ttl == 0 {
		opts.ttl = time.Hour
	}
	if opts.maxAge == 0 {
		opts.maxAge = 24 * time.Hour
	}
	if opts.resetTTL == 0 {
		opts.resetTTL = time.Hour
	}
	clk := &clock{now: time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)}
	users := authtest.NewUsers()
	sessions := authtest.NewSessions()
	resets := authtest.NewResets()
	mail := &authtest.Mailer{}

	authSvc, err := auth.NewService(users, auth.Options{Hash: fastHash, Now: clk.Now})
	if err != nil {
		t.Fatal(err)
	}
	sessionSvc := auth.NewSessionService(sessions, auth.SessionOptions{
		TTL: opts.ttl, MaxAge: opts.maxAge, Now: clk.Now,
	})
	var mailer auth.Mailer = mail
	if opts.noMail {
		mailer = nil
	}
	resetSvc, err := auth.NewPasswordResetService(users, resets, sessionSvc, auth.ResetOptions{
		Hash: fastHash, TTL: opts.resetTTL, Now: clk.Now, Mailer: mailer,
	})
	if err != nil {
		t.Fatal(err)
	}
	limiter, err := ratelimit.NewMemory(ratelimit.Config{Burst: opts.burst, Interval: time.Hour, Now: clk.Now})
	if err != nil {
		t.Fatal(err)
	}
	tierlists := tierlist.NewService(tierlisttest.NewMemStore(), tierlisttest.Users{}, tierlist.ServiceDeps{Now: clk.Now})
	h := api.New(api.Deps{
		Tierlists:   tierlists,
		AuthService: authSvc,
		Sessions:    sessionSvc,
		Resets:      resetSvc,
		AuthLimit:   limiter,
		AuthLimitMiddleware: ratelimit.Middleware(limiter, func(r *http.Request) string {
			return "auth:" + r.URL.Path + ":ip:" + ratelimit.HashIP([]byte("test-salt"), r.RemoteAddr)
		}),
		SecureCookies: opts.secure,
		Log:           slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	return &authEnv{h: h, users: users, sessions: sessions, resets: resets, mail: mail, clock: clk}
}

// do sends a request. cookie may be nil; remote defaults to a fixed address.
func (e *authEnv) do(t *testing.T, method, path, body, csrf string, cookie *http.Cookie, remote string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, api.Prefix+path, strings.NewReader(body))
	if remote == "" {
		remote = "203.0.113.7:4000"
	}
	req.RemoteAddr = remote
	if csrf != "" {
		req.Header.Set(api.CSRFHeader, csrf)
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	return rec
}

// csrf fetches a CSRF token, optionally for a session cookie.
func (e *authEnv) csrf(t *testing.T, cookie *http.Cookie) string {
	t.Helper()
	rec := e.do(t, http.MethodGet, "/csrf", "", "", cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /csrf = %d %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Token string `json:"csrf_token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Token == "" {
		t.Fatalf("csrf body %q: %v", rec.Body.String(), err)
	}
	return body.Token
}

// register creates a user over HTTP and returns the decoded user, the session
// cookie and the CSRF token of the new session.
func (e *authEnv) register(t *testing.T, email, password, name string) (map[string]any, *http.Cookie, string) {
	t.Helper()
	rec := e.do(t, http.MethodPost, "/auth/register",
		`{"email":"`+email+`","password":"`+password+`","display_name":"`+name+`"}`, "", nil, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("register = %d %s", rec.Code, rec.Body.String())
	}
	body := decodeMap(t, rec)
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != auth.SessionCookieName {
		t.Fatalf("cookies = %+v", cookies)
	}
	csrf, _ := body["csrf_token"].(string)
	if csrf == "" {
		t.Fatal("register response has no csrf_token")
	}
	return body, cookies[0], csrf
}

func cookieHeader(c *httptest.ResponseRecorder) string {
	for _, value := range c.Header().Values("Set-Cookie") {
		if strings.HasPrefix(value, auth.SessionCookieName+"=") {
			return value
		}
	}
	return ""
}

func TestRegister(t *testing.T) {
	e := newAuthEnv(t, authEnvOptions{secure: true})

	user, cookie, csrf := e.register(t, "Anna@Example.COM", "correct-horse-battery", "Anna")
	if user["email"] != "anna@example.com" || user["display_name"] != "Anna" || user["email_verified"] != false {
		t.Fatalf("user = %v", user)
	}
	if user["id"] == "" {
		t.Error("id is empty")
	}
	if _, err := time.Parse(time.RFC3339, user["created_at"].(string)); err != nil {
		t.Errorf("created_at: %v", err)
	}

	// Cookie attributes from SPEC section 6.
	req := e.do(t, http.MethodGet, "/auth/me", "", "", cookie, "")
	me := decodeMap(t, req)
	if req.Code != http.StatusOK || me["email"] != "anna@example.com" || me["csrf_token"] != csrf {
		t.Fatalf("me = %d %v", req.Code, me)
	}

	if e.sessions.Len() != 1 {
		t.Errorf("sessions = %d", e.sessions.Len())
	}
}

func TestRegisterCookieAttributes(t *testing.T) {
	for _, secure := range []bool{true, false} {
		e := newAuthEnv(t, authEnvOptions{secure: secure})
		rec := e.do(t, http.MethodPost, "/auth/register",
			`{"email":"a@example.com","password":"correct-horse-battery","display_name":"A"}`, "", nil, "")
		if rec.Code != http.StatusCreated {
			t.Fatalf("register = %d %s", rec.Code, rec.Body.String())
		}
		setCookie := cookieHeader(rec)
		if setCookie == "" {
			t.Fatal("no session cookie")
		}
		for _, attr := range []string{"Path=/", "HttpOnly", "SameSite=Lax"} {
			if !strings.Contains(setCookie, attr) {
				t.Errorf("secure=%v: cookie %q lacks %q", secure, setCookie, attr)
			}
		}
		if got := strings.Contains(setCookie, "Secure"); got != secure {
			t.Errorf("secure=%v: cookie %q Secure=%v", secure, setCookie, got)
		}
		if strings.Contains(setCookie, "Max-Age") {
			t.Errorf("session cookie must not be persistent: %q", setCookie)
		}
	}
}

func TestRegisterErrors(t *testing.T) {
	e := newAuthEnv(t, authEnvOptions{})
	valid := `{"email":"anna@example.com","password":"correct-horse-battery","display_name":"Anna"}`

	if rec := e.do(t, http.MethodPost, "/auth/register", valid, "", nil, ""); rec.Code != http.StatusCreated {
		t.Fatalf("first register = %d", rec.Code)
	}
	m := wantError(t, e.do(t, http.MethodPost, "/auth/register", valid, "", nil, ""), 409, "email_taken")
	if m["message"] == "" {
		t.Error("no message")
	}

	tests := []struct {
		name   string
		body   string
		status int
		code   string
	}{
		{"bad json", `{`, http.StatusBadRequest, "bad_request"},
		{"unknown field", valid[:len(valid)-1] + `,"x":1}`, http.StatusBadRequest, "bad_request"},
		{"trailing", valid + `{}`, http.StatusBadRequest, "bad_request"},
		{"short password", `{"email":"b@example.com","password":"short","display_name":"B"}`, http.StatusUnprocessableEntity, "validation_failed"},
		{"bad email", `{"email":"nope","password":"correct-horse-battery","display_name":"B"}`, http.StatusUnprocessableEntity, "validation_failed"},
		{"blank name", `{"email":"b@example.com","password":"correct-horse-battery","display_name":"  "}`, http.StatusUnprocessableEntity, "validation_failed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wantError(t, e.do(t, http.MethodPost, "/auth/register", tt.body, "", nil, ""), tt.status, tt.code)
		})
	}
	if e.users.Len() != 1 {
		t.Errorf("users = %d, want 1", e.users.Len())
	}
}

func TestRegisterValidationDetails(t *testing.T) {
	e := newAuthEnv(t, authEnvOptions{})
	m := wantError(t, e.do(t, http.MethodPost, "/auth/register",
		`{"email":"anna@example.com","password":"short","display_name":"Anna"}`, "", nil, ""),
		http.StatusUnprocessableEntity, "validation_failed")
	fields, _ := m["details"].(map[string]any)["fields"].(map[string]any)
	if fields["password"] != "Длина пароля от 10 до 128 символов" {
		t.Errorf("fields = %v", fields)
	}
}

func TestLogin(t *testing.T) {
	e := newAuthEnv(t, authEnvOptions{})
	e.register(t, "anna@example.com", "correct-horse-battery", "Anna")

	rec := e.do(t, http.MethodPost, "/auth/login",
		`{"email":"ANNA@example.com","password":"correct-horse-battery"}`, "", nil, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("login = %d %s", rec.Code, rec.Body.String())
	}
	body := decodeMap(t, rec)
	if body["email"] != "anna@example.com" || body["csrf_token"] == "" {
		t.Errorf("body = %v", body)
	}
	if len(rec.Result().Cookies()) != 1 {
		t.Errorf("cookies = %+v", rec.Result().Cookies())
	}
	if e.sessions.Len() != 2 {
		t.Errorf("sessions = %d, want 2", e.sessions.Len())
	}

	for name, payload := range map[string]string{
		"wrong password": `{"email":"anna@example.com","password":"wrong-password-x"}`,
		"unknown email":  `{"email":"nobody@example.com","password":"correct-horse-battery"}`,
		"malformed":      `{"email":"nope","password":"x"}`,
	} {
		t.Run(name, func(t *testing.T) {
			wantError(t, e.do(t, http.MethodPost, "/auth/login", payload, "", nil, ""),
				http.StatusUnauthorized, "invalid_credentials")
		})
	}
	wantError(t, e.do(t, http.MethodPost, "/auth/login", `{`, "", nil, ""), http.StatusBadRequest, "bad_request")
}

func TestLogout(t *testing.T) {
	e := newAuthEnv(t, authEnvOptions{})
	_, cookie, csrf := e.register(t, "anna@example.com", "correct-horse-battery", "Anna")

	wantError(t, e.do(t, http.MethodPost, "/auth/logout", "", "", cookie, ""), http.StatusForbidden, "csrf_failed")
	wantError(t, e.do(t, http.MethodPost, "/auth/logout", "", "wrong-token", cookie, ""), http.StatusForbidden, "csrf_failed")

	rec := e.do(t, http.MethodPost, "/auth/logout", "", csrf, cookie, "")
	if rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
		t.Fatalf("logout = %d %q", rec.Code, rec.Body.String())
	}
	cleared := cookieHeader(rec)
	for _, attr := range []string{"Max-Age=0", "HttpOnly", "SameSite=Lax"} {
		if !strings.Contains(cleared, attr) {
			t.Errorf("cleared cookie %q lacks %q", cleared, attr)
		}
	}
	if rec := e.do(t, http.MethodGet, "/auth/me", "", "", cookie, ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("me after logout = %d", rec.Code)
	}
	// Logout is idempotent: the revoked session still gets 204 and a cleared
	// cookie when a fresh guest token is supplied.
	guest := e.csrf(t, cookie)
	if rec := e.do(t, http.MethodPost, "/auth/logout", "", guest, cookie, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("second logout = %d %s", rec.Code, rec.Body.String())
	}
}

func TestLogoutAll(t *testing.T) {
	e := newAuthEnv(t, authEnvOptions{})
	_, first, firstCSRF := e.register(t, "anna@example.com", "correct-horse-battery", "Anna")
	rec := e.do(t, http.MethodPost, "/auth/login", `{"email":"anna@example.com","password":"correct-horse-battery"}`, "", nil, "")
	if rec.Code != http.StatusOK {
		t.Fatal(rec.Body.String())
	}
	second := rec.Result().Cookies()[0]

	wantError(t, e.do(t, http.MethodPost, "/auth/logout-all", "", "", first, ""), http.StatusForbidden, "csrf_failed")
	if rec := e.do(t, http.MethodPost, "/auth/logout-all", "", firstCSRF, first, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("logout-all = %d %s", rec.Code, rec.Body.String())
	}
	for _, c := range []*http.Cookie{first, second} {
		if rec := e.do(t, http.MethodGet, "/auth/me", "", "", c, ""); rec.Code != http.StatusUnauthorized {
			t.Errorf("me after logout-all = %d", rec.Code)
		}
	}
}

func TestCSRFTokens(t *testing.T) {
	e := newAuthEnv(t, authEnvOptions{})

	guestA := e.csrf(t, nil)
	guestB := e.csrf(t, nil)
	if guestA == guestB || guestA == "" {
		t.Errorf("guest tokens must differ: %q %q", guestA, guestB)
	}

	_, cookieA, tokenA := e.register(t, "anna@example.com", "correct-horse-battery", "Anna")
	_, cookieB, tokenB := e.register(t, "bob@example.com", "correct-horse-battery", "Bob")
	if tokenA == tokenB {
		t.Error("session tokens must differ")
	}
	if got := e.csrf(t, cookieA); got != tokenA {
		t.Errorf("GET /csrf with session = %q, want %q", got, tokenA)
	}

	// Own token passes; a token of another session or a guest token with a
	// live session fails (M1: the session is bound to its own token).
	if rec := e.do(t, http.MethodPost, "/auth/logout", "", tokenA, cookieA, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("own token = %d %s", rec.Code, rec.Body.String())
	}
	wantError(t, e.do(t, http.MethodPost, "/auth/logout", "", guestA, cookieB, ""), http.StatusForbidden, "csrf_failed")
	if rec := e.do(t, http.MethodPost, "/auth/logout", "", tokenB, cookieB, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("session token = %d %s", rec.Code, rec.Body.String())
	}
	_, _, tokenC := e.register(t, "carol@example.com", "correct-horse-battery", "Carol")
	_, cookieD, _ := e.register(t, "dave@example.com", "correct-horse-battery", "Dave")
	wantError(t, e.do(t, http.MethodPost, "/auth/logout", "", tokenC, cookieD, ""), http.StatusForbidden, "csrf_failed")
}

func TestSessionExpiryAndRevocation(t *testing.T) {
	e := newAuthEnv(t, authEnvOptions{ttl: time.Hour, maxAge: 24 * time.Hour})
	_, cookie, _ := e.register(t, "anna@example.com", "correct-horse-battery", "Anna")

	e.clock.Advance(30 * time.Minute)
	if rec := e.do(t, http.MethodGet, "/auth/me", "", "", cookie, ""); rec.Code != http.StatusOK {
		t.Fatalf("me at 30m = %d", rec.Code)
	}
	e.clock.Advance(31 * time.Minute)
	if rec := e.do(t, http.MethodGet, "/auth/me", "", "", cookie, ""); rec.Code != http.StatusOK {
		t.Fatalf("me at 61m = %d", rec.Code)
	}

	// The cookie stops resolving once the absolute ceiling is reached, but
	// logout still answers 204 and clears the cookie (L1).
	e.clock.Advance(23 * time.Hour)
	if rec := e.do(t, http.MethodGet, "/auth/me", "", "", cookie, ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("me after absolute cap = %d", rec.Code)
	}
	guest := e.csrf(t, cookie)
	rec := e.do(t, http.MethodPost, "/auth/logout", "", guest, cookie, "")
	if rec.Code != http.StatusNoContent || cookieHeader(rec) == "" {
		t.Fatalf("logout after cap = %d %q", rec.Code, rec.Body.String())
	}
}

func TestForgotAndResetPassword(t *testing.T) {
	e := newAuthEnv(t, authEnvOptions{})
	_, sessionA, _ := e.register(t, "anna@example.com", "correct-horse-battery", "Anna")
	rec := e.do(t, http.MethodPost, "/auth/login", `{"email":"anna@example.com","password":"correct-horse-battery"}`, "", nil, "")
	if rec.Code != http.StatusOK {
		t.Fatal(rec.Body.String())
	}
	sessionB := rec.Result().Cookies()[0]

	// Unknown addresses get the same 202 and no email.
	guestCSRF := e.csrf(t, nil)
	if rec := e.do(t, http.MethodPost, "/auth/forgot-password", `{"email":"nobody@example.com"}`, guestCSRF, nil, ""); rec.Code != http.StatusAccepted {
		t.Fatalf("forgot unknown = %d", rec.Code)
	}
	if len(e.mail.Sent) != 0 {
		t.Errorf("mail sent for unknown address: %+v", e.mail.Sent)
	}
	wantError(t, e.do(t, http.MethodPost, "/auth/forgot-password", `{"email":"anna@example.com"}`, "", nil, ""), http.StatusForbidden, "csrf_failed")

	for _, payload := range []string{`{"email":"anna@example.com"}`, `{"email":"ANNA@example.com"}`} {
		if rec := e.do(t, http.MethodPost, "/auth/forgot-password", payload, guestCSRF, nil, ""); rec.Code != http.StatusAccepted {
			t.Fatalf("forgot = %d %s", rec.Code, rec.Body.String())
		}
	}
	mail, ok := e.mail.Last()
	if !ok || mail.To != "anna@example.com" || mail.Token == "" {
		t.Fatalf("mail = %+v, %v", mail, ok)
	}
	// The second request invalidates the first link.
	if e.resets.Len() == 0 {
		t.Fatal("no reset tokens")
	}

	// A weak password does not burn the token.
	wantError(t, e.do(t, http.MethodPost, "/auth/reset-password",
		`{"token":"`+mail.Token+`","new_password":"short"}`, guestCSRF, nil, ""),
		http.StatusUnprocessableEntity, "validation_failed")
	wantError(t, e.do(t, http.MethodPost, "/auth/reset-password",
		`{"token":"not-a-token","new_password":"another-long-password"}`, guestCSRF, nil, ""),
		http.StatusBadRequest, "invalid_token")

	if rec := e.do(t, http.MethodPost, "/auth/reset-password",
		`{"token":"`+mail.Token+`","new_password":"another-long-password"}`, guestCSRF, nil, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("reset = %d %s", rec.Code, rec.Body.String())
	}
	// Every session is gone.
	for _, c := range []*http.Cookie{sessionA, sessionB} {
		if rec := e.do(t, http.MethodGet, "/auth/me", "", "", c, ""); rec.Code != http.StatusUnauthorized {
			t.Errorf("session survived reset: %d", rec.Code)
		}
	}
	// The token is single-use; the old password is dead, the new one works.
	wantError(t, e.do(t, http.MethodPost, "/auth/reset-password",
		`{"token":"`+mail.Token+`","new_password":"third-long-password"}`, guestCSRF, nil, ""),
		http.StatusBadRequest, "invalid_token")
	wantError(t, e.do(t, http.MethodPost, "/auth/login",
		`{"email":"anna@example.com","password":"correct-horse-battery"}`, "", nil, ""),
		http.StatusUnauthorized, "invalid_credentials")
	if rec := e.do(t, http.MethodPost, "/auth/login",
		`{"email":"anna@example.com","password":"another-long-password"}`, "", nil, ""); rec.Code != http.StatusOK {
		t.Fatalf("login after reset = %d %s", rec.Code, rec.Body.String())
	}
}

func TestResetTokenExpires(t *testing.T) {
	e := newAuthEnv(t, authEnvOptions{resetTTL: 15 * time.Minute})
	e.register(t, "anna@example.com", "correct-horse-battery", "Anna")
	guestCSRF := e.csrf(t, nil)
	if rec := e.do(t, http.MethodPost, "/auth/forgot-password", `{"email":"anna@example.com"}`, guestCSRF, nil, ""); rec.Code != http.StatusAccepted {
		t.Fatal(rec.Code)
	}
	mail, _ := e.mail.Last()
	e.clock.Advance(16 * time.Minute)
	wantError(t, e.do(t, http.MethodPost, "/auth/reset-password",
		`{"token":"`+mail.Token+`","new_password":"another-long-password"}`, guestCSRF, nil, ""),
		http.StatusBadRequest, "invalid_token")
}

func TestVerifyEmailStub(t *testing.T) {
	e := newAuthEnv(t, authEnvOptions{})
	guestCSRF := e.csrf(t, nil)
	wantError(t, e.do(t, http.MethodPost, "/auth/verify-email", `{"token":"abc"}`, "", nil, ""),
		http.StatusForbidden, "csrf_failed")
	wantError(t, e.do(t, http.MethodPost, "/auth/verify-email", `{"token":"abc"}`, guestCSRF, nil, ""),
		http.StatusBadRequest, "invalid_token")
	wantError(t, e.do(t, http.MethodPost, "/auth/verify-email", `{`, guestCSRF, nil, ""),
		http.StatusBadRequest, "bad_request")
}

func TestAuthRateLimitByIP(t *testing.T) {
	e := newAuthEnv(t, authEnvOptions{burst: 2})
	payload := `{"email":"anna@example.com","password":"wrong-password-x"}`
	for range 2 {
		if rec := e.do(t, http.MethodPost, "/auth/login", payload, "", nil, "198.51.100.1:1"); rec.Code != http.StatusUnauthorized {
			t.Fatalf("login = %d", rec.Code)
		}
	}
	rec := e.do(t, http.MethodPost, "/auth/login", payload, "", nil, "198.51.100.1:1")
	wantError(t, rec, http.StatusTooManyRequests, "rate_limited")
	if rec.Header().Get("Retry-After") == "" {
		t.Error("Retry-After is missing")
	}
	// Another IP and another endpoint have their own buckets.
	if rec := e.do(t, http.MethodPost, "/auth/login",
		`{"email":"bob@example.com","password":"wrong-password-x"}`, "", nil, "198.51.100.2:1"); rec.Code != http.StatusUnauthorized {
		t.Errorf("other IP = %d", rec.Code)
	}
	if rec := e.do(t, http.MethodPost, "/auth/register",
		`{"email":"bob@example.com","password":"correct-horse-battery","display_name":"Bob"}`, "", nil, "198.51.100.1:1"); rec.Code != http.StatusCreated {
		t.Errorf("register on the same IP = %d", rec.Code)
	}
}

func TestAuthRateLimitByEmail(t *testing.T) {
	e := newAuthEnv(t, authEnvOptions{burst: 3})
	payload := `{"email":"anna@example.com","password":"wrong-password-x"}`
	for i := range 3 {
		remote := "198.51.100." + string(rune('1'+i)) + ":1"
		if rec := e.do(t, http.MethodPost, "/auth/login", payload, "", nil, remote); rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d = %d", i, rec.Code)
		}
	}
	// Each IP used one token; the email bucket is exhausted.
	wantError(t, e.do(t, http.MethodPost, "/auth/login", payload, "", nil, "198.51.100.9:1"),
		http.StatusTooManyRequests, "rate_limited")
	// Another address is still allowed from that IP.
	if rec := e.do(t, http.MethodPost, "/auth/login",
		`{"email":"bob@example.com","password":"wrong-password-x"}`, "", nil, "198.51.100.9:1"); rec.Code != http.StatusUnauthorized {
		t.Errorf("other email = %d", rec.Code)
	}
}

func TestPasswordResetMailNotConfigured(t *testing.T) {
	e := newAuthEnv(t, authEnvOptions{noMail: true})
	guest := e.csrf(t, nil)

	// No token is created, no link is logged or sent.
	wantError(t, e.do(t, http.MethodPost, "/auth/forgot-password",
		`{"email":"nobody@example.com"}`, guest, nil, ""), http.StatusServiceUnavailable, "mail_not_configured")
	wantError(t, e.do(t, http.MethodPost, "/auth/forgot-password",
		`{"email":"anna@example.com"}`, guest, nil, ""), http.StatusServiceUnavailable, "mail_not_configured")
	if e.resets.Len() != 0 || len(e.mail.Sent) != 0 {
		t.Fatalf("tokens=%d mails=%d", e.resets.Len(), len(e.mail.Sent))
	}

	// Even a token that exists in the store cannot be used while mail is off.
	token := strings.Repeat("a", 43)
	if err := e.resets.Create(t.Context(), &auth.PasswordReset{
		TokenHash: auth.HashSessionToken(token),
		UserID:    "u1",
		CreatedAt: e.clock.Now(),
		ExpiresAt: e.clock.Now().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	wantError(t, e.do(t, http.MethodPost, "/auth/reset-password",
		`{"token":"`+token+`","new_password":"another-long-password"}`, guest, nil, ""),
		http.StatusServiceUnavailable, "mail_not_configured")
}

func TestResetErrorOrder(t *testing.T) {
	e := newAuthEnv(t, authEnvOptions{})
	guest := e.csrf(t, nil)

	// A weak password wins over an invalid token: 422, not 400.
	m := wantError(t, e.do(t, http.MethodPost, "/auth/reset-password",
		`{"token":"not-a-token","new_password":"short"}`, guest, nil, ""),
		http.StatusUnprocessableEntity, "validation_failed")
	fields, _ := m["details"].(map[string]any)["fields"].(map[string]any)
	if fields["password"] == nil {
		t.Errorf("details = %v", m["details"])
	}
}

func TestResetRevokeAllFailure(t *testing.T) {
	e := newAuthEnv(t, authEnvOptions{})
	_, session, _ := e.register(t, "anna@example.com", "correct-horse-battery", "Anna")
	guest := e.csrf(t, nil)
	if rec := e.do(t, http.MethodPost, "/auth/forgot-password",
		`{"email":"anna@example.com"}`, guest, nil, ""); rec.Code != http.StatusAccepted {
		t.Fatalf("forgot = %d", rec.Code)
	}
	mail, _ := e.mail.Last()

	// RevokeAll fails after the password changed and the token was consumed.
	// The handler reports 500; the token is burnt and the old sessions survive.
	e.sessions.Errs["RevokeAll"] = errors.New("db down")
	wantError(t, e.do(t, http.MethodPost, "/auth/reset-password",
		`{"token":"`+mail.Token+`","new_password":"another-long-password"}`, guest, nil, ""),
		http.StatusInternalServerError, "internal_error")
	delete(e.sessions.Errs, "RevokeAll")

	// The token is single-use even though the request failed.
	wantError(t, e.do(t, http.MethodPost, "/auth/reset-password",
		`{"token":"`+mail.Token+`","new_password":"third-long-password"}`, guest, nil, ""),
		http.StatusBadRequest, "invalid_token")
	// The session was not revoked (the failure is visible to the client).
	if rec := e.do(t, http.MethodGet, "/auth/me", "", "", session, ""); rec.Code != http.StatusOK {
		t.Errorf("session after failed revocation = %d", rec.Code)
	}
	// The password change itself is committed.
	wantError(t, e.do(t, http.MethodPost, "/auth/login",
		`{"email":"anna@example.com","password":"correct-horse-battery"}`, "", nil, ""),
		http.StatusUnauthorized, "invalid_credentials")
	if rec := e.do(t, http.MethodPost, "/auth/login",
		`{"email":"anna@example.com","password":"another-long-password"}`, "", nil, ""); rec.Code != http.StatusOK {
		t.Fatalf("login with new password = %d %s", rec.Code, rec.Body.String())
	}
}

func TestSessionReadOncePerRequest(t *testing.T) {
	e := newAuthEnv(t, authEnvOptions{})
	_, cookie, csrf := e.register(t, "anna@example.com", "correct-horse-battery", "Anna")
	before := e.sessions.GetCalls()

	// withSession authenticates once; guardCSRF and requireUser reuse it.
	if rec := e.do(t, http.MethodPost, "/auth/logout-all", "", csrf, cookie, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("logout-all = %d %s", rec.Code, rec.Body.String())
	}
	if got := e.sessions.GetCalls(); got != before+1 {
		t.Fatalf("session reads = %d, want %d", got, before+1)
	}
}

func TestNoAuthenticator(t *testing.T) {
	// api.New must not panic and must treat every request as a guest.
	h := api.New(api.Deps{
		Tierlists: tierlist.NewService(tierlisttest.NewMemStore(), tierlisttest.Users{}, tierlist.ServiceDeps{}),
	})
	req := httptest.NewRequest(http.MethodPost, api.Prefix+"/tierlists", strings.NewReader(createBody))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	wantError(t, rec, http.StatusUnauthorized, "unauthorized")

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, api.Prefix+"/csrf", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("csrf without authenticator = %d", rec.Code)
	}
}
