package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"code.sarnas.ru/tierl/internal/media"
	"code.sarnas.ru/tierl/internal/tierlist"
	"code.sarnas.ru/tierl/internal/tierlist/tierlisttest"
	"code.sarnas.ru/tierl/internal/web/api"
)

func newMediaEnv(t *testing.T, lim *media.Limits) (env, *media.FS) {
	t.Helper()
	store := tierlisttest.NewMemStore()
	users := tierlisttest.Users{
		"anna": {DisplayName: "Anna", EmailVerified: true},
		"bob":  {DisplayName: "Bob", EmailVerified: true},
	}
	svc := tierlist.NewService(store, users, tierlist.ServiceDeps{
		Now: func() time.Time { return time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC) },
	})
	fs, err := media.NewFS(filepath.Join(t.TempDir(), "media"), "/media/")
	if err != nil {
		t.Fatalf("NewFS: %v", err)
	}
	t.Cleanup(func() { _ = fs.Close() })
	h := api.New(api.Deps{
		Tierlists:   svc,
		Auth:        headerAuth{},
		Log:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		Media:       fs,
		MediaLimits: lim,
	})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, api.Prefix+"/csrf", nil))
	var tok struct {
		CSRF string `json:"csrf_token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &tok); err != nil || tok.CSRF == "" {
		t.Fatalf("GET /csrf: %d %s (%v)", rec.Code, rec.Body.String(), err)
	}
	return env{h: h, store: store, csrf: tok.CSRF}, fs
}

func samplePNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := range 8 {
		for x := range 8 {
			img.Set(x, y, color.RGBA{R: uint8(x * 20), G: uint8(y * 20), B: 120, A: 255}) //nolint:gosec // test pixels
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatalf("png encode: %v", err)
	}
	return b.Bytes()
}

func uploadFile(t *testing.T, h http.Handler, csrf, user, name string, data []byte) (int, map[string]any) {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, err := mw.CreateFormFile("file", name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, api.Prefix+"/media", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set(api.CSRFHeader, csrf)
	if user != "" {
		req.Header.Set("X-Test-User", user)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	out := map[string]any{}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func mediaKey(t *testing.T, raw string) string {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse url %q: %v", raw, err)
	}
	return strings.TrimPrefix(u.Path, "/media/")
}

func TestMediaUploadServeAndDelete(t *testing.T) {
	e, fs := newMediaEnv(t, nil)
	pngData := samplePNG(t)

	code, out := uploadFile(t, e.h, e.csrf, "anna", "a.png", pngData)
	if code != http.StatusOK {
		t.Fatalf("upload: %d %v", code, out)
	}
	raw, _ := out["url"].(string)
	if !strings.HasPrefix(raw, "/media/users/anna/") {
		t.Fatalf("url = %q", raw)
	}

	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, u.Path, nil))
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "image/") || rec.Body.Len() == 0 {
		t.Fatalf("GET media: %d %q %d bytes", rec.Code, rec.Header().Get("Content-Type"), rec.Body.Len())
	}

	if code, _ := uploadFile(t, e.h, e.csrf, "", "a.png", pngData); code != http.StatusUnauthorized {
		t.Fatalf("guest upload = %d, want 401", code)
	}
	if code, _ := uploadFile(t, e.h, e.csrf, "anna", "a.txt", []byte("not an image")); code != http.StatusUnsupportedMediaType {
		t.Fatalf("text upload = %d, want 415", code)
	}
	corrupt := append([]byte(nil), pngData...)
	corrupt[len(corrupt)/2] ^= 0xff
	if code, _ := uploadFile(t, e.h, e.csrf, "anna", "broken.png", corrupt); code != http.StatusBadRequest {
		t.Fatalf("corrupt upload = %d, want 400", code)
	}

	key := mediaKey(t, raw)
	if _, err := fs.Get(context.Background(), key); err != nil {
		t.Fatalf("stored object: %v", err)
	}
}

func TestMediaUploadTooLarge(t *testing.T) {
	e, _ := newMediaEnv(t, &media.Limits{MaxBytes: 64, MaxSide: 8000, MaxPixels: 40_000_000, OutSide: 1024})
	if code, _ := uploadFile(t, e.h, e.csrf, "anna", "big.png", samplePNG(t)); code != http.StatusRequestEntityTooLarge {
		t.Fatalf("large upload = %d, want 413", code)
	}
}

func TestMediaItemLifecycle(t *testing.T) {
	e, fs := newMediaEnv(t, nil)
	ctx := context.Background()

	code, uploaded := uploadFile(t, e.h, e.csrf, "anna", "a.png", samplePNG(t))
	if code != http.StatusOK {
		t.Fatalf("upload: %d %v", code, uploaded)
	}
	annaURL := uploaded["url"].(string)
	code, bobUpload := uploadFile(t, e.h, e.csrf, "bob", "b.png", samplePNG(t))
	if code != http.StatusOK {
		t.Fatalf("bob upload: %d %v", code, bobUpload)
	}
	bobURL := bobUpload["url"].(string)

	foreign := fmt.Sprintf(`{"title":"T","visibility":"public","slug":"foreign-pic",
"tiers":[{"name":"S","color":"#ff0000"},{"name":"A","color":"#00ff00"}],
"items":[{"label":"a","image_url":%q},{"label":"b"},{"label":"c"}]}`, bobURL)
	if rec := e.do(t, http.MethodPost, "/tierlists", "anna", foreign); rec.Code != http.StatusBadRequest {
		t.Fatalf("foreign URL create = %d %s", rec.Code, rec.Body.String())
	}

	body := fmt.Sprintf(`{"title":"T","visibility":"public","slug":"pic-list",
"tiers":[{"name":"S","color":"#ff0000"},{"name":"A","color":"#00ff00"}],
"items":[{"label":"a","image_url":%q},{"label":"b"},{"label":"c"}]}`, annaURL)
	rec := e.do(t, http.MethodPost, "/tierlists", "anna", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	key := mediaKey(t, annaURL)
	if _, err := fs.Get(ctx, key); err != nil {
		t.Fatalf("stored object: %v", err)
	}

	patch := `{"items":[{"label":"a"},{"label":"b"},{"label":"c"}]}`
	if rec := e.do(t, http.MethodPatch, "/tierlists/pic-list", "anna", patch); rec.Code != http.StatusOK {
		t.Fatalf("patch: %d %s", rec.Code, rec.Body.String())
	}
	if _, err := fs.Get(ctx, key); !errors.Is(err, media.ErrNotFound) {
		t.Fatalf("removed item file still exists: %v", err)
	}

	code, uploaded = uploadFile(t, e.h, e.csrf, "anna", "c.png", samplePNG(t))
	if code != http.StatusOK {
		t.Fatalf("second upload: %d %v", code, uploaded)
	}
	secondURL := uploaded["url"].(string)
	body = fmt.Sprintf(`{"title":"T2","visibility":"public","slug":"delete-pic",
"tiers":[{"name":"S","color":"#ff0000"},{"name":"A","color":"#00ff00"}],
"items":[{"label":"a","image_url":%q},{"label":"b"},{"label":"c"}]}`, secondURL)
	if rec := e.do(t, http.MethodPost, "/tierlists", "anna", body); rec.Code != http.StatusCreated {
		t.Fatalf("create second: %d %s", rec.Code, rec.Body.String())
	}
	if rec := e.do(t, http.MethodDelete, "/tierlists/delete-pic", "anna", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("delete tierlist: %d %s", rec.Code, rec.Body.String())
	}
	if _, err := fs.Get(ctx, mediaKey(t, secondURL)); !errors.Is(err, media.ErrNotFound) {
		t.Fatalf("deleted tierlist file still exists: %v", err)
	}
}
