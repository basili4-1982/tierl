// Package api implements the JSON HTTP API described in api/openapi.yaml.
// Handlers only translate HTTP to service calls and back: business rules live
// in internal/tierlist and there is no SQL here.
package api

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"code.sarnas.ru/tierl/internal/auth"
	"code.sarnas.ru/tierl/internal/domain"
	"code.sarnas.ru/tierl/internal/media"
	"code.sarnas.ru/tierl/internal/platform/ratelimit"
	"code.sarnas.ru/tierl/internal/tierlist"
)

// Prefix is the path prefix of every API route (servers.url in the contract).
const Prefix = "/api/v1"

// maxBodyBytes bounds a JSON request body.
const maxBodyBytes = 1 << 20

// Authenticator identifies the signed-in user of a request.
type Authenticator interface {
	// UserID returns the ID of the signed-in user and true, or false for guests.
	UserID(r *http.Request) (string, bool)
}

// Middleware wraps a handler, for example with a rate limiter.
type Middleware func(http.Handler) http.Handler

// Deps are the dependencies of the API handler.
type Deps struct {
	Tierlists *tierlist.Service
	Auth      Authenticator
	Log       *slog.Logger
	// Accounts serves the email confirmation and password reset endpoints;
	// nil leaves them unregistered (for example without a database).
	Accounts Accounts
	// CreateTierlistLimit wraps POST /tierlists; nil means no extra middleware.
	CreateTierlistLimit Middleware
	// ResendVerificationLimit wraps POST /auth/resend-verification.
	ResendVerificationLimit Middleware
	// ForgotPasswordLimit wraps POST /auth/forgot-password.
	ForgotPasswordLimit Middleware
	// VerifyEmailLimit wraps POST /auth/verify-email.
	VerifyEmailLimit Middleware
	// ResetPasswordLimit wraps POST /auth/reset-password.
	ResetPasswordLimit Middleware
	// EmailLimiter limits forgot/resend by the email in the body; the IP
	// middleware above still applies. Nil disables the per-email dimension.
	EmailLimiter ratelimit.Limiter
	// Store and Results are optional; they let the share pages and OG images
	// resolve tier lists and participant results. Production wires both.
	Store   tierlist.Store
	Results ResultProvider
	Users   tierlist.Users
	// AuthService and Sessions enable the auth endpoints. Both must be set.
	AuthService *auth.Service
	Sessions    *auth.SessionManager
	// CSRF overrides the process-wide CSRF token source; nil uses the default.
	CSRF *auth.CSRF
	// ReportHideThreshold overrides the number of reports that hides a public
	// tier list automatically. Zero keeps the service/default value.
	ReportHideThreshold int
	// HideReportsThreshold is an alias for ReportHideThreshold.
	HideReportsThreshold int
	// ReportThreshold and ReportsThreshold are aliases.
	ReportThreshold  int
	ReportsThreshold int
	// Media stores item pictures. Nil leaves POST /media unavailable (503).
	Media media.Storage
	// ImageStorage and MediaStore are aliases for Media kept for callers that
	// name the dependency by use.
	ImageStorage media.Storage
	MediaStore   media.Storage
	// MediaLimits overrides media.DefaultLimits; nil uses the defaults.
	MediaLimits *media.Limits
}

// API is the HTTP handler of the JSON API.
type API struct {
	tierlists    *tierlist.Service
	auth         Authenticator
	accounts     Accounts
	emailLimiter ratelimit.Limiter
	log          *slog.Logger
	mux          *http.ServeMux
	store        tierlist.Store
	results      ResultProvider
	users        tierlist.Users
	authService  *auth.Service
	sessions     *auth.SessionManager
	csrf         *auth.CSRF
	media        media.Storage
	mediaLimits  media.Limits
}

// New builds the handler with all routes registered under Prefix.
func New(d Deps) *API {
	a := &API{
		tierlists: d.Tierlists, auth: d.Auth, accounts: d.Accounts,
		emailLimiter: d.EmailLimiter, log: d.Log, mux: http.NewServeMux(),
		store: d.Store, results: d.Results, users: d.Users,
		authService: d.AuthService, sessions: d.Sessions, csrf: d.CSRF,
		media: d.Media, mediaLimits: media.DefaultLimits(),
	}
	if a.media == nil {
		a.media = d.ImageStorage
	}
	if a.media == nil {
		a.media = d.MediaStore
	}
	if d.MediaLimits != nil {
		a.mediaLimits = *d.MediaLimits
	}
	if a.log == nil {
		a.log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if a.csrf == nil {
		a.csrf = auth.NewCSRF(nil)
	}
	if a.tierlists != nil {
		threshold := d.ReportHideThreshold
		for _, n := range []int{d.HideReportsThreshold, d.ReportThreshold, d.ReportsThreshold} {
			if threshold <= 0 {
				threshold = n
			}
		}
		if threshold > 0 {
			a.tierlists.SetReportHideThreshold(threshold)
		}
	}

	create := http.Handler(http.HandlerFunc(a.createTierlist))
	if d.CreateTierlistLimit != nil {
		create = d.CreateTierlistLimit(create)
	}
	a.mux.HandleFunc("GET "+Prefix+"/csrf", a.getCSRF)
	a.mux.HandleFunc("GET "+Prefix+"/catalog", a.listCatalog)
	a.mux.HandleFunc("GET /sitemap.xml", a.sitemap)
	if a.authService != nil && a.sessions != nil {
		a.mux.HandleFunc("POST "+Prefix+"/auth/register", a.register)
		a.mux.HandleFunc("POST "+Prefix+"/auth/login", a.login)
		a.mux.HandleFunc("POST "+Prefix+"/auth/logout", a.logout)
		a.mux.HandleFunc("POST "+Prefix+"/auth/logout-all", a.logoutAll)
		a.mux.HandleFunc("POST "+Prefix+"/auth/change-password", a.changePassword)
		a.mux.HandleFunc("PATCH "+Prefix+"/auth/password", a.changePassword)
		a.mux.HandleFunc("POST "+Prefix+"/me/password", a.changeMyPassword)
		a.mux.HandleFunc("GET "+Prefix+"/auth/me", a.me)
	}
	a.mux.HandleFunc("GET "+Prefix+"/me/tierlists", a.listMyTierlists)
	if a.accounts != nil {
		a.mux.Handle("POST "+Prefix+"/auth/verify-email",
			withMiddleware(http.HandlerFunc(a.verifyEmail), d.VerifyEmailLimit))
		a.mux.Handle("POST "+Prefix+"/auth/reset-password",
			withMiddleware(http.HandlerFunc(a.resetPassword), d.ResetPasswordLimit))
		a.mux.Handle("POST "+Prefix+"/auth/resend-verification",
			withMiddleware(http.HandlerFunc(a.resendVerification), d.ResendVerificationLimit))
		a.mux.Handle("POST "+Prefix+"/auth/forgot-password",
			withMiddleware(http.HandlerFunc(a.forgotPassword), d.ForgotPasswordLimit))
	}
	a.mux.Handle("POST "+Prefix+"/tierlists", create)
	a.mux.HandleFunc("POST "+Prefix+"/media", a.uploadMedia)
	a.mux.HandleFunc("GET "+Prefix+"/media/{key...}", a.serveMedia)
	a.mux.HandleFunc("GET "+Prefix+"/tierlists/{slug}", a.getTierlist)
	a.mux.HandleFunc("PATCH "+Prefix+"/tierlists/{slug}", a.updateTierlist)
	a.mux.HandleFunc("DELETE "+Prefix+"/tierlists/{slug}", a.deleteTierlist)
	a.mux.HandleFunc("POST "+Prefix+"/tierlists/{slug}/close", a.closeTierlist)
	a.mux.HandleFunc("POST "+Prefix+"/tierlists/{slug}/reports", a.createReport)
	a.mux.HandleFunc("POST "+Prefix+"/tierlists/{slug}/submissions", a.createSubmission)
	a.mux.HandleFunc("PUT "+Prefix+"/tierlists/{slug}/submissions/mine", a.replaceSubmission)
	a.mux.HandleFunc("GET "+Prefix+"/tierlists/{slug}/submissions/mine", a.getMySubmission)
	a.mux.HandleFunc("GET "+Prefix+"/results/{id}", a.getResult)
	a.mux.HandleFunc("GET "+Prefix+"/results/{id}/stats", a.getResultStats)
	a.mountShareRoutes()
	a.mux.HandleFunc(Prefix+"/", func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusNotFound, "not_found", "Не найдено", nil)
	})
	return a
}

// withMiddleware wraps h when mw is not nil.
func withMiddleware(h http.Handler, mw Middleware) http.Handler {
	if mw == nil {
		return h
	}
	return mw(h)
}

// ServeHTTP implements http.Handler. Every unsafe request must carry a valid
// X-CSRF-Token header before it reaches a handler.
func (a *API) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if base, ok := a.mediaBasePath(); ok && strings.HasPrefix(r.URL.Path, base) {
		a.serveMedia(w, r)
		return
	}
	if isUnsafeMethod(r.Method) && !a.csrf.Valid(r.Header.Get(CSRFHeader)) && !a.accountsOnlyEmailRequest(r) {
		writeError(w, http.StatusForbidden, "csrf_failed", "Неверный CSRF-токен", nil)
		return
	}
	a.mux.ServeHTTP(w, r)
}

// accountsOnlyEmailRequest keeps the email endpoints usable in a build that
// wires only an Accounts implementation (no browser session and no CSRF
// source). The production setup always configures sessions, so the full API
// keeps enforcing CSRF for every unsafe request.
func (a *API) accountsOnlyEmailRequest(r *http.Request) bool {
	if a.authService != nil || a.sessions != nil {
		return false
	}
	switch r.URL.Path {
	case Prefix + "/auth/verify-email", Prefix + "/auth/resend-verification",
		Prefix + "/auth/forgot-password", Prefix + "/auth/reset-password":
		return true
	default:
		return false
	}
}

// errorBody is the Error schema of the contract.
type errorBody struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

func writeError(w http.ResponseWriter, status int, code, message string, details map[string]any) {
	writeJSON(w, status, errorBody{Code: code, Message: message, Details: details})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// requireUser returns the signed-in user or writes 401.
func (a *API) requireUser(w http.ResponseWriter, r *http.Request) (string, bool) {
	id, ok := a.auth.UserID(r)
	if !ok || id == "" {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Требуется вход", nil)
		return "", false
	}
	return id, true
}

// decode reads a JSON body into dst and writes 400 on any syntax problem.
func decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Некорректное тело запроса", nil)
		return false
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "bad_request", "Некорректное тело запроса", nil)
		return false
	}
	return true
}

// writeServiceError maps service and domain errors to HTTP responses.
func (a *API) writeServiceError(w http.ResponseWriter, r *http.Request, err error) {
	var verr *domain.ValidationError
	switch {
	case errors.Is(err, tierlist.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "Не найдено", nil)
	case errors.Is(err, tierlist.ErrBadCursor), errors.Is(err, tierlist.ErrBadSort):
		writeError(w, http.StatusBadRequest, "bad_request", "Некорректный параметр запроса", nil)
	case errors.Is(err, tierlist.ErrForbidden):
		writeError(w, http.StatusForbidden, "forbidden", "Нет прав на этот опрос", nil)
	case errors.Is(err, tierlist.ErrNotModerator):
		writeError(w, http.StatusForbidden, "forbidden", "Требуются права модератора", nil)
	case errors.Is(err, tierlist.ErrReportDuplicate):
		writeError(w, http.StatusConflict, "report_duplicate", "Вы уже жаловались на этот опрос", nil)
	case errors.Is(err, tierlist.ErrTierlistHidden), errors.Is(err, tierlist.ErrReportNotFound):
		writeError(w, http.StatusNotFound, "not_found", "Не найдено", nil)
	case errors.Is(err, tierlist.ErrEmailNotVerified):
		writeError(w, http.StatusForbidden, "email_not_verified", "Подтвердите email, чтобы создавать опросы", nil)
	case errors.Is(err, domain.ErrStructureLocked):
		writeError(w, http.StatusConflict, "tierlist_locked", "Опрос уже проходили, структуру менять нельзя", nil)
	case errors.Is(err, tierlist.ErrAlreadySubmitted):
		var already *tierlist.AlreadySubmittedError
		details := map[string]any(nil)
		if errors.As(err, &already) {
			details = map[string]any{"result_id": already.ResultID}
		}
		writeError(w, http.StatusConflict, "already_submitted", "Вы уже отвечали на этот опрос", details)
	case errors.Is(err, tierlist.ErrNotAcceptingSubmissions):
		writeError(w, http.StatusConflict, "tierlist_closed", "Опрос закрыт", nil)
	case errors.Is(err, tierlist.ErrSlugTaken):
		writeError(w, http.StatusConflict, "slug_taken", "Этот адрес уже занят", map[string]any{"fields": map[string]string{"slug": "уже занят"}})
	case errors.Is(err, domain.ErrInvalidTransition):
		writeError(w, http.StatusConflict, "invalid_status", "Недопустимая смена статуса опроса", nil)
	case errors.As(err, &verr):
		writeError(w, http.StatusUnprocessableEntity, "validation_failed", "Проверьте поля формы",
			map[string]any{"fields": map[string]string{verr.Field: verr.Reason}})
	case errors.Is(err, domain.ErrInvalid):
		writeError(w, http.StatusUnprocessableEntity, "validation_failed", "Проверьте поля формы", nil)
	default:
		a.log.ErrorContext(r.Context(), "request failed", slog.String("path", r.URL.Path), slog.Any("error", err))
		writeError(w, http.StatusInternalServerError, "internal_error", "Внутренняя ошибка", nil)
	}
}
