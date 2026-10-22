package static

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func testFS() fstest.MapFS {
	return fstest.MapFS{
		"index.html":      {Data: []byte("<html>spa</html>")},
		"assets/app.js":   {Data: []byte("console.log(1)")},
		"assets/dir/x.js": {Data: []byte("x")},
	}
}

func get(t *testing.T, h http.Handler, method, target string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, target, nil))
	return rec
}

func TestHandler(t *testing.T) {
	h := Handler(testFS())
	tests := []struct {
		name   string
		method string
		target string
		code   int
		body   string
	}{
		{"root", http.MethodGet, "/", 200, "<html>spa</html>"},
		{"asset", http.MethodGet, "/assets/app.js", 200, "console.log(1)"},
		{"spa route", http.MethodGet, "/t/some-slug", 200, "<html>spa</html>"},
		{"directory falls back", http.MethodGet, "/assets/dir", 200, "<html>spa</html>"},
		{"explicit index", http.MethodGet, "/index.html", 301, ""},
		{"missing asset", http.MethodGet, "/assets/missing.js", 404, ""},
		{"api reserved", http.MethodGet, "/api/v1/polls", 404, ""},
		{"api exact", http.MethodGet, "/api", 404, ""},
		{"healthz reserved", http.MethodGet, "/healthz", 404, ""},
		{"traversal cleaned", http.MethodGet, "/a/../t/x", 200, "<html>spa</html>"},
		{"head", http.MethodHead, "/t/x", 200, ""},
		{"post", http.MethodPost, "/t/x", 405, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := get(t, h, tt.method, tt.target)
			if rec.Code != tt.code {
				t.Fatalf("status = %d, want %d", rec.Code, tt.code)
			}
			if tt.body != "" && rec.Body.String() != tt.body {
				t.Errorf("body = %q, want %q", rec.Body.String(), tt.body)
			}
		})
	}
}

func TestHandler_NotBuilt(t *testing.T) {
	rec := get(t, Handler(fstest.MapFS{}), http.MethodGet, "/")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestEmbeddedFS(t *testing.T) {
	// The embedded dist may hold only a placeholder; the handler must not panic.
	rec := get(t, Handler(FS()), http.MethodGet, "/some/route")
	if rec.Code != http.StatusOK && rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d", rec.Code)
	}
}
