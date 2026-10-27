package api

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"

	"code.sarnas.ru/tierl/internal/domain"
	"code.sarnas.ru/tierl/internal/media"
)

// mediaUploadSlack is the room for multipart headers and boundaries on top of
// the image size limit.
const mediaUploadSlack = 1 << 20

// mediaBasePath returns the path prefix the storage serves objects from, for
// example "/media/". It returns false for an absolute URL (S3/CDN): those
// objects are served outside the application.
func (a *API) mediaBasePath() (string, bool) {
	if a.media == nil {
		return "", false
	}
	base := a.media.URL("")
	if base == "" || !strings.HasPrefix(base, "/") {
		return "", false
	}
	if !strings.HasSuffix(base, "/") {
		base += "/"
	}
	if u, err := url.Parse(base); err != nil || u.Scheme != "" || u.Host != "" {
		return "", false
	}
	return base, true
}

// MediaHandler lets the root handler serve files of a filesystem storage.
// The first result is the path prefix to mount, empty for external storages.
func (a *API) MediaHandler() (string, http.Handler) {
	base, ok := a.mediaBasePath()
	if !ok {
		return "", nil
	}
	return base, http.HandlerFunc(a.serveMedia)
}

// CloseMedia releases the filesystem storage, if any.
func (a *API) CloseMedia() error {
	if a.media == nil {
		return nil
	}
	if c, ok := a.media.(io.Closer); ok {
		return c.Close()
	}
	return nil
}

// uploadMedia implements POST /media: it validates the multipart file through
// media.Process, stores the re-encoded bytes and returns { "url": "..." }.
func (a *API) uploadMedia(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	if a.media == nil {
		writeError(w, http.StatusServiceUnavailable, "media_unavailable", "Загрузка картинок не настроена", nil)
		return
	}

	lim := a.mediaLimits
	r.Body = http.MaxBytesReader(w, r.Body, lim.MaxBytes+mediaUploadSlack)
	if err := r.ParseMultipartForm(mediaUploadSlack); err != nil { //nolint:gosec // total body is capped by MaxBytesReader above
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "file_too_large", "Файл слишком большой", nil)
			return
		}
		writeError(w, http.StatusBadRequest, "bad_request", "Некорректная multipart-форма", nil)
		return
	}
	if r.MultipartForm != nil {
		defer func() { _ = r.MultipartForm.RemoveAll() }()
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Поле file обязательно", nil)
		return
	}
	defer func() { _ = file.Close() }()

	if header.Size > lim.MaxBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "file_too_large", "Файл слишком большой", nil)
		return
	}
	raw, err := io.ReadAll(io.LimitReader(file, lim.MaxBytes+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Не удалось прочитать файл", nil)
		return
	}
	if int64(len(raw)) > lim.MaxBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "file_too_large", "Файл слишком большой", nil)
		return
	}

	data, contentType, err := media.Process(bytes.NewReader(raw), lim)
	if err != nil {
		a.writeMediaError(w, r, err)
		return
	}
	key := "users/" + userID + "/" + media.Key(data, contentType)
	if err := a.media.Put(r.Context(), key, data, contentType); err != nil {
		a.log.ErrorContext(r.Context(), "store media", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "Внутренняя ошибка", nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"url": a.media.URL(key)})
}

func (a *API) writeMediaError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, media.ErrTooLarge), errors.Is(err, media.ErrDimensionsTooLarge):
		writeError(w, http.StatusRequestEntityTooLarge, "file_too_large", "Файл слишком большой", nil)
	case errors.Is(err, media.ErrUnsupportedType):
		writeError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "Поддерживаются только PNG, JPEG, WebP и GIF", nil)
	case errors.Is(err, media.ErrEmpty), errors.Is(err, media.ErrCorrupt):
		writeError(w, http.StatusBadRequest, "corrupt_image", "Файл повреждён или не является картинкой", nil)
	default:
		a.log.ErrorContext(r.Context(), "process media", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "Внутренняя ошибка", nil)
	}
}

// serveMedia serves an object from the filesystem storage by its public path.
func (a *API) serveMedia(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	key, ok := a.mediaKeyFromPath(r.URL.Path)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "Не найдено", nil)
		return
	}
	rc, err := a.media.Get(r.Context(), key)
	if errors.Is(err, media.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Не найдено", nil)
		return
	}
	if err != nil {
		a.log.ErrorContext(r.Context(), "read media", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "Внутренняя ошибка", nil)
		return
	}
	defer func() { _ = rc.Close() }()
	data, err := io.ReadAll(rc)
	if err != nil {
		a.log.ErrorContext(r.Context(), "read media", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "Внутренняя ошибка", nil)
		return
	}

	contentType := mime.TypeByExtension(path.Ext(key))
	if contentType == "" {
		contentType = http.DetectContentType(data)
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(data)
}

func (a *API) mediaKeyFromPath(p string) (string, bool) {
	prefixes := []string{Prefix + "/media/"}
	if base, ok := a.mediaBasePath(); ok {
		prefixes = append(prefixes, base)
	}
	for _, prefix := range prefixes {
		if strings.HasPrefix(p, prefix) {
			key := strings.TrimPrefix(p, prefix)
			if media.ValidateKey(key) == nil {
				return key, true
			}
		}
	}
	return "", false
}

// mediaKeyFromURL extracts the object key from a URL produced by store.URL.
func mediaKeyFromURL(raw string, store media.Storage) (string, bool) {
	if raw == "" || store == nil {
		return "", false
	}
	base := strings.TrimRight(store.URL(""), "/")
	if base != "" && strings.HasPrefix(raw, base+"/") {
		key := strings.TrimPrefix(raw, base+"/")
		if media.ValidateKey(key) == nil {
			return key, true
		}
		return "", false
	}
	if base != "" {
		return "", false
	}
	// A storage without a public base (some tests use the key as the URL).
	if media.ValidateKey(raw) == nil {
		return raw, true
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", false
	}
	key := strings.TrimPrefix(u.Path, "/")
	if strings.HasPrefix(key, "users/") && media.ValidateKey(key) == nil {
		return key, true
	}
	return "", false
}

// ownedMediaKey returns the key of an upload of userID. An empty URL is valid
// and yields an empty key.
func (a *API) ownedMediaKey(userID, raw string) (string, bool) {
	if raw == "" {
		return "", true
	}
	key, ok := mediaKeyFromURL(raw, a.media)
	if !ok || !strings.HasPrefix(key, "users/"+userID+"/") {
		return "", false
	}
	return key, true
}

// validateItemImages rejects foreign or external image URLs when storage is
// configured: only files returned to this user by POST /media may be attached.
func (a *API) validateItemImages(w http.ResponseWriter, userID string, items []itemInputDTO) bool {
	if a.media == nil {
		return true
	}
	for _, it := range items {
		if it.ImageURL == nil || *it.ImageURL == "" {
			continue
		}
		if _, ok := a.ownedMediaKey(userID, *it.ImageURL); !ok {
			writeError(w, http.StatusBadRequest, "bad_image_url", "Можно привязать только загруженную вами картинку",
				map[string]any{"fields": map[string]string{"item.image_url": "чужой или внешний URL"}})
			return false
		}
	}
	return true
}

// deleteImages removes the files of urls that belong to userID. Errors are
// logged: a storage problem must not cancel an already stored tier list change.
func (a *API) deleteImages(ctx context.Context, userID string, urls []string) {
	if a.media == nil {
		return
	}
	seen := make(map[string]struct{}, len(urls))
	for _, raw := range urls {
		if raw == "" {
			continue
		}
		key, ok := a.ownedMediaKey(userID, raw)
		if !ok {
			continue
		}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		if err := a.media.Delete(ctx, key); err != nil {
			a.log.WarnContext(ctx, "delete media", "key", key, "error", err)
		}
	}
}

// deleteRemovedImages removes files of old items that are no longer referenced
// by the new structure (removed elements and replaced pictures).
func (a *API) deleteRemovedImages(ctx context.Context, userID string, oldItems, newItems []domain.Item) {
	if a.media == nil {
		return
	}
	keep := make(map[string]struct{}, len(newItems))
	for _, it := range newItems {
		if it.ImageURL != "" {
			keep[it.ImageURL] = struct{}{}
		}
	}
	var removed []string
	for _, it := range oldItems {
		if it.ImageURL == "" {
			continue
		}
		if _, ok := keep[it.ImageURL]; !ok {
			removed = append(removed, it.ImageURL)
		}
	}
	a.deleteImages(ctx, userID, removed)
}

func itemImageURLs(items []domain.Item) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		if it.ImageURL != "" {
			out = append(out, it.ImageURL)
		}
	}
	return out
}
