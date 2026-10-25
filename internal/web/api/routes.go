package api

import (
	"net/http"
	"strings"
)

// defaultCSRFExempt are the public state-changing routes: login and
// registration do not act on an existing session.
var defaultCSRFExempt = []string{
	"POST " + Prefix + "/auth/login",
	"POST " + Prefix + "/auth/register",
}

// authRequiredRoutes are routes that answer 401 without a session. CSRF is
// checked after authentication for them, so a guest gets 401 and an
// authenticated caller without a token gets 403.
var authRequiredRoutes = []string{
	"POST " + Prefix + "/tierlists",
	"PATCH " + Prefix + "/tierlists/{slug}",
	"DELETE " + Prefix + "/tierlists/{slug}",
	"POST " + Prefix + "/tierlists/{slug}/close",
	"POST " + Prefix + "/auth/logout-all",
}

// isUnsafeMethod reports whether a method needs CSRF protection.
func isUnsafeMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

// routeSet matches requests against Go 1.22 ServeMux patterns such as
// "POST /api/v1/tierlists/{slug}/close". It reuses ServeMux matching instead of
// a hand-written path parser.
type routeSet struct {
	mux *http.ServeMux
}

func newRouteSet(patterns []string) *routeSet {
	s := &routeSet{mux: http.NewServeMux()}
	for _, pattern := range patterns {
		if strings.TrimSpace(pattern) == "" {
			continue
		}
		s.mux.Handle(pattern, http.NotFoundHandler())
	}
	return s
}

// has reports whether the request matches one of the patterns.
func (s *routeSet) has(r *http.Request) bool {
	if s == nil {
		return false
	}
	_, pattern := s.mux.Handler(r)
	return pattern != ""
}
