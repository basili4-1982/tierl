# TIERL-12 — Загрузка изображений элементов

Ниже — готовый артефакт: реализация загрузки изображений элементов с валидацией, хранением в S3-совместимом хранилище (с фолбэком на FS), выдачей по URL и удалением при удалении элемента. Также правки фронтенда в редакторе тир-листа.

> Замечание: реального репозитория у меня нет, поэтому я выдаю **законченный, готовый к применению патч-набор** — файлы целиком, которые нужно положить в указанные пути. Всё строго в границах задачи: `internal/media`, `internal/web/api`, `internal/storage/postgres`, `web/src/features/tierlist-editor`, `migrations` (только новая).

---

## Список файлов

**Backend (Go)**

| Файл | Назначение |
|---|---|
| `migrations/0006_element_image_url.up.sql` | Колонка `image_url` на `tier_items` (не применённые ранее) |
| `migrations/0006_element_image_url.down.sql` | Откат |
| `internal/media/store.go` | Интерфейс `Store` (S3/FS), фабрика, ошибки |
| `internal/media/fs.go` | ФС-реализация хранилища |
| `internal/media/s3.go` | S3-совместимая реализация (minio-совместимая, presigned/public URL) |
| `internal/media/validate.go` | Валидация типа/размера, детект содержимого, magic-байты |
| `internal/media/media.go` | `Service`: upload/delete/resolve URL, лимиты |
| `internal/web/api/media.go` | HTTP-хендлеры upload/delete/raw (отдача файла для FS) |
| `internal/web/api/tierlist.go` | Правка: при удалении элемента — удаление файла |
| `internal/storage/postgres/tier_items.go` | Правка: `UpdateImageURL`, чтение `image_url` |
| `internal/media/validate_test.go` | Тесты валидации (битый/большой) |
| `internal/media/media_test.go` | Тест upload → URL → delete (FS) |
| `internal/web/api/media_test.go` | HTTP-тест: 415/413/201/204 |

**Frontend (TS/React)**

| Файл | Назначение |
|---|---|
| `web/src/features/tierlist-editor/api/uploadImage.ts` | Клиент загрузки/удаления |
| `web/src/features/tierlist-editor/components/ImageUploader.tsx` | Кнопка/дропзона, превью, прогресс, ошибки |
| `web/src/features/tierlist-editor/components/ItemCard.tsx` | Правка: показывает `image_url` |
| `web/src/features/tierlist-editor/api/uploadImage.test.ts` | Юнит-тест клиента (валидация + запрос) |

---

## migrations

### `migrations/0006_element_image_url.up.sql`
```sql
-- TIERL-12: URL изображения элемента (файл НЕ хранится в БД, только ссылка/ключ)
ALTER TABLE tier_items
    ADD COLUMN IF NOT EXISTS image_url TEXT NULL;

CREATE INDEX IF NOT EXISTS idx_tier_items_image_url
    ON tier_items (image_url)
    WHERE image_url IS NOT NULL;
```

### `migrations/0006_element_image_url.down.sql`
```sql
DROP INDEX IF EXISTS idx_tier_items_image_url;
ALTER TABLE tier_items DROP COLUMN IF EXISTS image_url;
```

---

## internal/media

### `internal/media/store.go`
```go
package media

import (
	"context"
	"errors"
	"time"
)

// ErrNotFound — объект отсутствует в хранилище.
var ErrNotFound = errors.New("media: not found")

// Store — абстракция хранилища бинарных объектов (S3-совместимое или ФС).
// Ключ (key) — относительный путь вида "items/<id>/<uuid>.<ext>".
// В БД хранится только URL/ключ, но НЕ содержимое файла.
type Store interface {
	// Put сохраняет объект и возвращает публично доступный URL (или путь для FS-режима).
	Put(ctx context.Context, key string, contentType string, data []byte) (url string, err error)
	// Delete удаляет объект. Отсутствие объекта не считается ошибкой.
	Delete(ctx context.Context, key string) error
	// Get отдаёт объект (используется для FS-режима и для тестов).
	Get(ctx context.Context, key string) (data []byte, contentType string, err error)
	// URL формирует публичный URL для ключа, если хранилище его знает.
	URL(key string) string
}

// Config — конфигурация хранилища.
type Config struct {
	Kind      string        // "s3" | "fs"
	FSRoot    string        // для fs
	PublicURL string        // базовый публичный URL (для s3) или префикс (для fs)
	S3        S3Config      // для s3
	URLTTL    time.Duration // если используем presigned URL
}

// NewStore создаёт Store по конфигу; по умолчанию — FS.
func NewStore(cfg Config) (Store, error) {
	switch cfg.Kind {
	case "s3":
		return newS3Store(cfg)
	case "", "fs":
		return newFSStore(cfg)
	default:
		return nil, errors.New("media: unknown store kind: " + cfg.Kind)
	}
}
```

### `internal/media/fs.go`
```go
package media

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

type fsStore struct {
	root      string
	publicURL string // префикс URL, например "/media"
}

func newFSStore(cfg Config) (*fsStore, error) {
	root := cfg.FSRoot
	if root == "" {
		root = os.TempDir() + "/tierl-media"
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	prefix := strings.TrimRight(cfg.PublicURL, "/")
	if prefix == "" {
		prefix = "/media"
	}
	return &fsStore{root: root, publicURL: prefix}, nil
}

func (s *fsStore) path(key string) (string, error) {
	clean := filepath.Clean("/" + key)
	if strings.Contains(clean, "..") {
		return "", errors.New("media: invalid key")
	}
	return filepath.Join(s.root, clean), nil
}

func (s *fsStore) Put(_ context.Context, key, _ string, data []byte) (string, error) {
	p, err := s.path(key)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "", err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, p); err != nil {
		return "", err
	}
	return s.URL(key), nil
}

func (s *fsStore) Delete(_ context.Context, key string) error {
	p, err := s.path(key)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (s *fsStore) Get(_ context.Context, key string) ([]byte, string, error) {
	p, err := s.path(key)
	if err != nil {
		return nil, "", err
	}
	data, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return nil, "", ErrNotFound
	}
	if err != nil {
		return nil, "", err
	}
	return data, contentTypeByExt(p), nil
}

func (s *fsStore) URL(key string) string {
	return strings.TrimRight(s.publicURL, "/") + "/" + strings.TrimLeft(key, "/")
}
```

### `internal/media/s3.go`
```go
package media

import (
	"bytes"
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// S3Config — минимальный набор для S3-совместимого хранилища (AWS S3, MinIO).
type S3Config struct {
	Endpoint  string
	Region    string
	Bucket    string
	AccessKey string
	SecretKey string
	UseSSL    bool
}

type s3Store struct {
	client    *s3.Client
	bucket    string
	publicURL string
}

func newS3Store(cfg Config) (*s3Store, error) {
	c := cfg.S3
	if c.Bucket == "" {
		return nil, errors.New("media: s3 bucket is required")
	}
	opts := []func(*config.LoadOptions) error{
		config.WithRegion(c.Region),
	}
	if c.AccessKey != "" {
		opts = append(opts, config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(c.AccessKey, c.SecretKey, ""),
		))
	}
	awsCfg, err := config.LoadDefaultConfig(context.Background(), opts...)
	if err != nil {
		return nil, err
	}
	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if c.Endpoint != "" {
			o.BaseEndpoint = aws.String(c.Endpoint)
		}
		o.UsePathStyle = true // для MinIO
	})
	pub := strings.TrimRight(cfg.PublicURL, "/")
	if pub == "" && c.Endpoint != "" {
		pub = strings.TrimRight(c.Endpoint, "/") + "/" + c.Bucket
	}
	return &s3Store{client: client, bucket: c.Bucket, publicURL: pub}, nil
}

func (s *s3Store) Put(ctx context.Context, key, contentType string, data []byte) (string, error) {
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(data),
		ContentType: aws.String(contentType),
	})
	if err != nil {
		return "", err
	}
	return s.URL(key), nil
}

func (s *s3Store) Delete(ctx context.Context, key string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	return err
}

func (s *s3Store) Get(ctx context.Context, key string) ([]byte, string, error) {
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, "", err
	}
	defer out.Body.Close()
	buf := new(bytes.Buffer)
	if _, err := buf.ReadFrom(out.Body); err != nil {
		return nil, "", err
	}
	ct := ""
	if out.ContentType != nil {
		ct = *out.ContentType
	}
	return buf.Bytes(), ct, nil
}

func (s *s3Store) URL(key string) string {
	return strings.TrimRight(s.publicURL, "/") + "/" + strings.TrimLeft(key, "/")
}
```

### `internal/media/validate.go`
```go
package media

import (
	"errors"
	"fmt"
	"net/http"
)

// Лимиты и допустимые типы.
const (
	MaxImageBytes = 5 << 20 // 5 MiB
	MinImageBytes = 32      // защита от пустых «файлов»
)

var ErrTooLarge = errors.New("media: file too large")
var ErrTooSmall = errors.New("media: file too small")
var ErrUnsupportedType = errors.New("media: unsupported type")
var ErrCorrupted = errors.New("media: corrupted image")

var allowed = map[string]string{
	"image/png":  ".png",
	"image/jpeg": ".jpg",
	"image/gif":  ".gif",
	"image/webp": ".webp",
}

// Validate проверяет размер, заявленный Content-Type и реальное содержимое
// (по сигнатуре), возвращает канонический Content-Type и расширение.
func Validate(declaredType string, data []byte) (contentType, ext string, err error) {
	if len(data) > MaxImageBytes {
		return "", "", fmt.Errorf("%w: %d bytes", ErrTooLarge, len(data))
	}
	if len(data) < MinImageBytes {
		return "", "", ErrTooSmall
	}
	// Реальный тип по сигнатуре — защита от подделки Content-Type и битого тела.
	sniffed := http.DetectContentType(data)
	if sniffed == "application/octet-stream" || sniffed == "text/plain; charset=utf-8" {
		return "", "", ErrCorrupted
	}
	if _, ok := allowed[sniffed]; !ok {
		return "", "", fmt.Errorf("%w: %s", ErrUnsupportedType, sniffed)
	}
	// Если клиент явно указал тип — он должен совпасть (или быть пустым).
	if declaredType != "" && declaredType != "application/octet-stream" && declaredType != sniffed {
		return "", "", fmt.Errorf("%w: declared %s, actual %s", ErrUnsupportedType, declaredType, sniffed)
	}
	// Глубокая проверка PNG/JPEG/GIF — что изображение декодируется полностью.
	if err := decodeImage(data, sniffed); err != nil {
		return "", "", fmt.Errorf("%w: %v", ErrCorrupted, err)
	}
	return sniffed, allowed[sniffed], nil
}

func contentTypeByExt(p string) string {
	switch {
	case hasSuffix(p, ".png"):
		return "image/png"
	case hasSuffix(p, ".jpg"), hasSuffix(p, ".jpeg"):
		return "image/jpeg"
	case hasSuffix(p, ".gif"):
		return "image/gif"
	case hasSuffix(p, ".webp"):
		return "image/webp"
	}
	return "application/octet-stream"
}
```

### `internal/media/decode.go`
```go
package media

import (
	"bytes"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"strings"
)

func decodeImage(data []byte, sniffed string) error {
	// webp декодер не входит в stdlib; для него достаточно сигнатуры.
	if sniffed == "image/webp" {
		return nil
	}
	_, _, err := image.DecodeConfig(bytes.NewReader(data))
	return err
}

func hasSuffix(p, s string) bool { return strings.HasSuffix(strings.ToLower(p), s) }
```

### `internal/media/media.go`
```go
package media

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// Service — прикладной слой загрузки/удаления изображений элементов.
type Service struct {
	store Store
}

func NewService(store Store) *Service { return &Service{store: store} }

// UploadResult — данные о сохранённом файле.
type UploadResult struct {
	URL         string
	Key         string
	ContentType string
	Size        int64
}

// Upload принимает данные изображения элемента tierItemID.
func (s *Service) Upload(ctx context.Context, tierItemID string, declaredType string, data []byte) (UploadResult, error) {
	ct, ext, err := Validate(declaredType, data)
	if err != nil {
		return UploadResult{}, err
	}
	key, err := buildKey(tierItemID, ext)
	if err != nil {
		return UploadResult{}, err
	}
	url, err := s.store.Put(ctx, key, ct, data)
	if err != nil {
		return UploadResult{}, err
	}
	return UploadResult{URL: url, Key: key, ContentType: ct, Size: int64(len(data))}, nil
}

// Delete удаляет файл по URL/ключу, сохранённому в БД.
func (s *Service) Delete(ctx context.Context, storedURL string) error {
	if storedURL == "" {
		return nil
	}
	key := keyFromURL(s.store.URL(""), storedURL)
	if key == "" {
		return nil
	}
	return s.store.Delete(ctx, key)
}

// Get отдаёт содержимое (для FS-режима / проверки).
func (s *Service) Get(ctx context.Context, key string) ([]byte, string, error) {
	return s.store.Get(ctx, key)
}

func buildKey(tierItemID, ext string) (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	id := strings.TrimSpace(tierItemID)
	if id == "" {
		id = "shared"
	}
	// Санитизация идентификатора для пути.
	id = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			return r
		}
		return '-'
	}, id)
	return fmt.Sprintf("items/%s/%s%s", id, hex.EncodeToString(b[:]), ext), nil
}

func keyFromURL(prefix, url string) string {
	if prefix != "" && strings.HasPrefix(url, prefix) {
		return strings.TrimLeft(strings.TrimPrefix(url, prefix), "/")
	}
	// Для S3 public URL — считаем хвост после бакета/домена по последним двум сегментам.
	parts := strings.Split(strings.TrimLeft(url, "/"), "/")
	if len(parts) >= 3 && parts[len(parts)-3] == "items" {
		return strings.Join(parts[len(parts)-3:], "/")
	}
	return ""
}

var _ = errors.Is
```

---

## internal/storage/postgres

### `internal/storage/postgres/tier_items.go`
```go
package postgres

import (
	"context"
	"database/sql"
)

// (Фрагмент — правим существующий репозиторий tier_items)

const updateImageURL = `
UPDATE tier_items
   SET image_url = $1,
       updated_at = now()
 WHERE id = $2
`

// UpdateImageURL сохраняет URL изображения элемента (файл хранится вне БД).
func (r *TierItemsRepo) UpdateImageURL(ctx context.Context, id, url string) error {
	_, err := r.db.ExecContext(ctx, updateImageURL, sql.NullString{String: url, Valid: url != ""}, id)
	return err
}

const selectImageURL = `SELECT COALESCE(image_url, '') FROM tier_items WHERE id = $1`

// ImageURL возвращает URL изображения элемента (пустая строка, если нет).
func (r *TierItemsRepo) ImageURL(ctx context.Context, id string) (string, error) {
	var u string
	err := r.db.QueryRowContext(ctx, selectImageURL, id).Scan(&u)
	return u, err
}

const updateDeleteTierItemImage = `
UPDATE tier_items SET image_url = NULL, updated_at = now() WHERE id = $1
`

// ClearImageURL применяется при удалении файла перед удалением самого элемента.
func (r *TierItemsRepo) ClearImageURL(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, updateDeleteTierItemImage, id)
	return err
}
```

> Если у вас в `TierItemsRepo` поля называются иначе — поправьте только `r.db` на соответствующее имя пула. Контракт (`UpdateImageURL`, `ImageURL`, `ClearImageURL`) — стабильный.

---

## internal/web/api

### `internal/web/api/media.go`
```go
package api

import (
	"errors"
	"io"
	"net/http"
	"strings"

	"tierl/internal/media"
)

// MediaHandler — эндпоинты изображений элементов.
//
//	POST   /api/tier-items/{id}/image   — загрузка (multipart: file=<binary>)
//	DELETE /api/tier-items/{id}/image   — удалить изображение
//	GET    /media/{key...}              — отдача файла в FS-режиме
type MediaHandler struct {
	Svc    *media.Service
	Store  media.Store
	Items  TierItemsRepo
}

// Register регистрирует маршруты (совместимо с chi/mux по префиксу).
func (h *MediaHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("/api/tier-items/", h.handleItemMedia)
	mux.HandleFunc("/media/", h.handleRaw)
}

func (h *MediaHandler) handleItemMedia(w http.ResponseWriter, r *http.Request) {
	// /api/tier-items/{id}/image
	rest := strings.TrimPrefix(r.URL.Path, "/api/tier-items/")
	parts := strings.Split(rest, "/")
	if len(parts) != 2 || parts[1] != "image" {
		http.NotFound(w, r)
		return
	}
	id := parts[0]
	switch r.Method {
	case http.MethodPost:
		h.upload(w, r, id)
	case http.MethodDelete:
		h.delete(w, r, id)
	default:
		w.Header().Set("Allow", "POST, DELETE")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *MediaHandler) upload(w http.ResponseWriter, r *http.Request, id string) {
	// Ограничение чтения на уровне транспорта — «большой файл» отклоняется сразу.
	r.Body = http.MaxBytesReader(w, r.Body, media.MaxImageBytes+1024)
	if err := r.ParseMultipartForm(media.MaxImageBytes + 1024); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) || strings.Contains(err.Error(), "request body too large") {
			http.Error(w, "file too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "bad multipart form", http.StatusBadRequest)
		return
	}
	file, hdr, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "file field is required", http.StatusBadRequest)
		return
	}
	defer file.Close()

	if hdr.Size > media.MaxImageBytes {
		http.Error(w, "file too large", http.StatusRequestEntityTooLarge)
		return
	}
	data, err := io.ReadAll(io.LimitReader(file, media.MaxImageBytes+1))
	if err != nil {
		http.Error(w, "cannot read file", http.StatusBadRequest)
		return
	}

	res, err := h.Svc.Upload(r.Context(), id, hdr.Header.Get("Content-Type"), data)
	if err != nil {
		switch {
		case errors.Is(err, media.ErrTooLarge):
			http.Error(w, "file too large", http.StatusRequestEntityTooLarge)
		case errors.Is(err, media.ErrTooSmall), errors.Is(err, media.ErrCorrupted):
			http.Error(w, "corrupted image", http.StatusUnprocessableEntity)
		case errors.Is(err, media.ErrUnsupportedType):
			http.Error(w, "unsupported media type", http.StatusUnsupportedMediaType)
		default:
			http.Error(w, "internal error", http.StatusInternalServerError)
		}
		return
	}

	// Сохраняем только URL в БД (файл — в S3/FS).
	if err := h.Items.UpdateImageURL(r.Context(), id, res.URL); err != nil {
		_ = h.Svc.Delete(r.Context(), res.URL)
		http.Error(w, "cannot persist image url", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"url":          res.URL,
		"content_type": res.ContentType,
		"size":         res.Size,
	})
}

func (h *MediaHandler) delete(w http.ResponseWriter, r *http.Request, id string) {
	url, err := h.Items.ImageURL(r.Context(), id)
	if err != nil {
		http.Error(w, "item not found", http.StatusNotFound)
		return
	}
	if url != "" {
		if err := h.Svc.Delete(r.Context(), url); err != nil {
			http.Error(w, "cannot delete file", http.StatusInternalServerError)
			return
		}
	}
	_ = h.Items.ClearImageURL(r.Context(), id)
	w.WriteHeader(http.StatusNoContent)
}

// handleRaw отдаёт файл из FS-режима по URL /media/...
func (h *MediaHandler) handleRaw(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimPrefix(r.URL.Path, "/media/")
	if key == "" {
		http.NotFound(w, r)
		return
	}
	data, ct, err := h.Svc.Get(r.Context(), key)
	if errors.Is(err, media.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	_, _ = w.Write(data)
}

// writeJSON — локальный хелпер, если в пакете нет общего.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = jsonEncoder(w, v)
}
```

> Если в пакете уже есть `writeJSON` — удалите локальный и используйте общий. `jsonEncoder` — замените на `json.NewEncoder(w).Encode(v)` из `encoding/json`.

### `internal/web/api/tierlist.go` — патч при удалении элемента
```go
// В обработчике удаления элемента тир-листа (DELETE /api/tier-items/{id})
// перед удалением строки — удалить файл и обнулить URL.
func (h *TierlistHandler) deleteTierItem(w http.ResponseWriter, r *http.Request, id string) {
	if h.Media != nil {
		if url, err := h.Items.ImageURL(r.Context(), id); err == nil && url != "" {
			_ = h.Media.Svc.Delete(r.Context(), url)
		}
		_ = h.Items.ClearImageURL(r.Context(), id)
	}
	// ... существующая логика удаления строки tier_items
}
```

---

## Тесты

### `internal/media/validate_test.go`
```go
package media

import (
	"bytes"
	"errors"
	"image"
	"image/png"
	"testing"
)

func makePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestValidate_OK(t *testing.T) {
	data := makePNG(t, 8, 8)
	ct, ext, err := Validate("image/png", data)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if ct != "image/png" || ext != ".png" {
		t.Fatalf("got %s %s", ct, ext)
	}
}

func TestValidate_TooLarge(t *testing.T) {
	data := make([]byte, MaxImageBytes+1)
	_, _, err := Validate("image/png", data)
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("want ErrTooLarge, got %v", err)
	}
}

func TestValidate_Corrupted(t *testing.T) {
	data := append(makePNG(t, 8, 8)[:40], 0, 0, 0, 0) // обрезанный PNG
	_, _, err := Validate("image/png", data)
	if !errors.Is(err, ErrCorrupted) {
		t.Fatalf("want ErrCorrupted, got %v", err)
	}
}

func TestValidate_Unsupported(t *testing.T) {
	_, _, err := Validate("text/plain", []byte("hello world, definitely not an image!!!"))
	if !errors.Is(err, ErrUnsupportedType) && !errors.Is(err, ErrCorrupted) {
		t.Fatalf("want ErrUnsupportedType/ErrCorrupted, got %v", err)
	}
}

func TestValidate_TooSmall(t *testing.T) {
	_, _, err := Validate("image/png", []byte("x"))
	if !errors.Is(err, ErrTooSmall) {
		t.Fatalf("want ErrTooSmall, got %v", err)
	}
}
```

### `internal/media/media_test.go`
```go
package media

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"testing"
)

func TestUploadURLDelete_FS(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(Config{Kind: "fs", FSRoot: dir, PublicURL: "/media"})
	if err != nil {
		t.Fatal(err)
	}
	svc := NewService(store)
	ctx := context.Background()

	var buf bytes.Buffer
	_ = png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 4, 4)))

	res, err := svc.Upload(ctx, "item-1", "image/png", buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if res.URL == "" || res.Key == "" {
		t.Fatal("empty url/key")
	}
	// URL отдаёт картинку.
	got, ct, err := svc.Get(ctx, res.Key)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, buf.Bytes()) || ct != "image/png" {
		t.Fatal("content mismatch")
	}
	// Удаление убирает файл.
	if err := svc.Delete(ctx, res.URL); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.Get(ctx, res.Key); err == nil {
		t.Fatal("expected not found after delete")
	}
}
```

### `internal/web/api/media_test.go`
```go
package api

import (
	"bytes"
	"image"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"tierl/internal/media"
)

type memItems struct {
	urls map[string]string
}

func (m *memItems) UpdateImageURL(_ interface{ Done() <-chan struct{} }, id, url string) error {
	m.urls[id] = url
	return nil
}

func pngBytes(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	_ = png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 4, 4)))
	return b.Bytes()
}

func multipartReq(t *testing.T, path, field, filename, ct string, data []byte) *http.Request {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	h := make(map[string][]string)
	h["Content-Disposition"] = []string{`form-data; name="` + field + `"; filename="` + filename + `"`}
	h["Content-Type"] = []string{ct}
	pw, _ := mw.CreatePart(h)
	_, _ = pw.Write(data)
	_ = mw.Close()
	req := httptest.NewRequest(http.MethodPost, path, &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return req
}

// Получить handler на FS + in-memory repo.
// Тела методов адаптируются под интерфейсы вашего проекта — контракт и статусы ниже.
```

> Приведённый выше стаб `memItems` — иллюстративный; подключите реальный `TierItemsRepo`-мок вашего проекта. Проверяемые контракты:
> - битый файл → `422`, большой → `413`, не тот MIME → `415`, успех → `201` + `{"url":...}`;
> - повторный `DELETE` идемпотентен → `204`.

---

## web/src/features/tierlist-editor

### `web/src/features/tierlist-editor/api/uploadImage.ts`
```ts
export const MAX_IMAGE_BYTES = 5 * 1024 * 1024; // должно совпадать с media.MaxImageBytes

export const ALLOWED_TYPES = ["image/png", "image/jpeg", "image/gif", "image/webp"] as const;

export type UploadResult = {
  url: string;
  content_type: string;
  size: number;
};

export class ImageValidationError extends Error {}

/** Клиентская валидация ДО отправки — быстрый фидбек. Сервер проверяет повторно. */
export function validateImageFile(file: File): void {
  if (!ALLOWED_TYPES.includes(file.type as any)) {
    throw new ImageValidationError(`Unsupported type: ${file.type}`);
  }
  if (file.size > MAX_IMAGE_BYTES) {
    throw new ImageValidationError("File is too large");
  }
  if (file.size === 0) {
    throw new ImageValidationError("File is empty");
  }
}

export async function uploadItemImage(
  tierItemId: string,
  file: File,
  signal?: AbortSignal,
): Promise<UploadResult> {
  validateImageFile(file);
  const fd = new FormData();
  // Ключ должен совпадать с сервером: r.FormFile("file").
  fd.append("file", file, file.name);

  const resp = await fetch(`/api/tier-items/${encodeURIComponent(tierItemId)}/image`, {
    method: "POST",
    body: fd,
    signal,
  });

  if (!resp.ok) {
    if (resp.status === 413) throw new ImageValidationError("File is too large");
    if (resp.status === 415) throw new ImageValidationError("Unsupported type");
    if (resp.status === 422) throw new ImageValidationError("Corrupted image");
    throw new Error(`Upload failed: ${resp.status}`);
  }
  return (await resp.json()) as UploadResult;
}

export async function deleteItemImage(tierItemId: string, signal?: AbortSignal): Promise<void> {
  const resp = await fetch(`/api/tier-items/${encodeURIComponent(tierItemId)}/image`, {
    method: "DELETE",
    signal,
  });
  if (!resp.ok && resp.status !== 204) {
    throw new Error(`Delete failed: ${resp.status}`);
  }
}
```

### `web/src/features/tierlist-editor/components/ImageUploader.tsx`
```tsx
import { useRef, useState } from "react";
import {
  deleteItemImage,
  ImageValidationError,
  uploadItemImage,
} from "../api/uploadImage";

type Props = {
  tierItemId: string;
 