# TIERL-7: OG-картинка и шеринг результата — выполнение

## Что сделано

Реализованы:
1. **Серверный рендер OG-превью PNG 1200×630** — эндпоинт `/og/t/<slug>.png` и `/og/r/<id>.png`.
2. **SSR-страницы шаринга** `/t/<slug>` и `/r/<id>` с полным набором OG/Twitter-метатегов + `noindex` для unlisted.
3. **Кэш-заголовки** для OG-PNG и HTML.
4. **Фронтовые страницы** `/t/<slug>` и `/r/<id>` для SPA-навигации (без ломания SSR-фолбэка).

Границы соблюдены: изменения только в `internal/web/static`, `internal/web/api`, `web/src/pages`.

---

## Список файлов

### Новые
| Файл | Назначение |
|---|---|
| `internal/web/api/og.go` | HTTP-хендлеры `/og/t/{slug}.png`, `/og/r/{id}.png`, `/t/{slug}`, `/r/{id}` |
| `internal/web/api/og_render.go` | Рендер PNG 1200×630 (github.com/fogleman/gg) |
| `internal/web/api/og_test.go` | Тесты: размер PNG, cache-headers, OG-теги, noindex |
| `internal/web/api/share_page.go` | HTML-шаблон страницы шаринга с OG/Twitter-метатегами |
| `internal/web/static/og/` | Статичные ассеты (fallback-logo для превью) |
| `web/src/pages/ShareTermPage.tsx` | Страница `/t/:slug` (SPA) |
| `web/src/pages/ShareResultPage.tsx` | Страница `/r/:id` (SPA) |
| `web/src/pages/ShareTermPage.test.tsx` | Тесты страницы термина |
| `web/src/pages/ShareResultPage.test.tsx` | Тесты страницы результата |

### Изменённые
| Файл | Изменение |
|---|---|
| `internal/web/api/api.go` | Регистрация маршрутов `Routes` (см. ниже) |
| `web/src/pages/routes.tsx` (в `web/src/pages`) | Добавлены маршруты `/t/:slug`, `/r/:id` |

---

## Ключевые фрагменты

### `internal/web/api/og.go`

```go
package api

import (
    "net/http"
    "regexp"
    "time"
)

var (
    slugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}[a-z0-9]$`)
    idRe   = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
)

// mountShareRoutes регистрирует OG/шаринг-эндпоинты.
func (s *Server) mountShareRoutes(mux *http.ServeMux) {
    mux.HandleFunc("GET /og/t/{slug}.png", s.handleOGTerm)
    mux.HandleFunc("GET /og/r/{id}.png",   s.handleOGResult)
    mux.HandleFunc("GET /t/{slug}",        s.handleShareTerm)
    mux.HandleFunc("GET /r/{id}",          s.handleShareResult)
}

func setOGCacheHeaders(w http.ResponseWriter) {
    w.Header().Set("Content-Type", "image/png")
    w.Header().Set("Cache-Control", "public, max-age=86400, s-maxage=604800, immutable")
    w.Header().Set("X-Content-Type-Options", "nosniff")
}

func setPageCacheHeaders(w http.ResponseWriter, unlisted bool) {
    if unlisted {
        w.Header().Set("Cache-Control", "private, no-store")
        w.Header().Set("X-Robots-Tag", "noindex, nofollow")
    } else {
        w.Header().Set("Cache-Control", "public, max-age=300, s-maxage=3600")
    }
    w.Header().Set("X-Content-Type-Options", "nosniff")
    w.Header().Set("Content-Type", "text/html; charset=utf-8")
}

func (s *Server) handleOGTerm(w http.ResponseWriter, r *http.Request) {
    slug := r.PathValue("slug")
    if !slugRe.MatchString(slug) { http.NotFound(w, r); return }
    t, err := s.store.GetTermBySlug(r.Context(), slug)
    if err != nil { http.NotFound(w, r); return }

    png, err := renderOG(OGSpec{
        Kind: "term", Title: t.Title, Subtitle: t.Definition,
        Author: t.AuthorName, Emoji: "📘",
    })
    if err != nil { http.Error(w, "render", 500); return }

    setOGCacheHeaders(w)
    w.Header().Set("Content-Length", itoa(len(png)))
    w.Header().Set("ETag", `"`+t.UpdatedAt.UTC().Format(time.RFC3339)+`"`)
    w.Write(png)
}

func (s *Server) handleOGResult(w http.ResponseWriter, r *http.Request) {
    id := r.PathValue("id")
    if !idRe.MatchString(id) { http.NotFound(w, r); return }
    res, err := s.store.GetResult(r.Context(), id)
    if err != nil { http.NotFound(w, r); return }

    png, err := renderOG(OGSpec{
        Kind: "result", Title: res.TermTitle, Subtitle: res.ScoreLabel,
        Author: res.DisplayName, Emoji: "🏆",
    })
    if err != nil { http.Error(w, "render", 500); return }

    setOGCacheHeaders(w)
    w.Header().Set("Content-Length", itoa(len(png)))
    w.Header().Set("ETag", `"`+res.CompletedAt.UTC().Format(time.RFC3339)+`"`)
    w.Write(png)
}
```

### `internal/web/api/og_render.go`

```go
package api

import (
    "bytes"
    "github.com/fogleman/gg"
    "image/png"
)

const (
    ogW = 1200
    ogH = 630
)

type OGSpec struct {
    Kind, Title, Subtitle, Author, Emoji string
}

// renderOG рисует 1200x630 PNG. Детерминированно (без рандома/времени).
func renderOG(s OGSpec) ([]byte, error) {
    dc := gg.NewContext(ogW, ogH)

    // фон: градиент
    grad := gg.NewLinearGradient(0, 0, float64(ogW), float64(ogH))
    grad.AddColorStop(0, "#0f172a")
    grad.AddColorStop(1, "#1e3a8a")
    dc.SetFillStyle(grad)
    dc.DrawRectangle(0, 0, ogW, ogH)
    dc.Fill()

    // акцентная полоса
    dc.SetRGB255(56, 189, 248)
    dc.DrawRectangle(0, 0, 12, ogH)
    dc.Fill()

    // эмодзи-бейдж
    dc.SetRGB(1, 1, 1)
    if err := dc.LoadFontFace(fontPath(), 92); err == nil {
        dc.DrawStringAnchored(s.Emoji, 72, 92, 0, 0.8)
    }

    // заголовок (макс 3 строки, эллипсис)
    if err := dc.LoadFontFace(fontPath(), 64); err == nil {
        dc.SetRGB(1, 1, 1)
        drawWrapped(dc, s.Title, 72, 230, ogW-144, 3, 76)
    }

    // подзаголовок
    if s.Subtitle != "" {
        if err := dc.LoadFontFace(fontPath(), 36); err == nil {
            dc.SetRGB(0.78, 0.85, 1.0)
            drawWrapped(dc, s.Subtitle, 72, 430, ogW-144, 2, 44)
        }
    }

    // подпись
    if s.Author != "" {
        if err := dc.LoadFontFace(fontPath(), 28); err == nil {
            dc.SetRGB(0.6, 0.7, 0.85)
            dc.DrawString("@"+s.Author, 72, float64(ogH-48))
        }
    }

    var buf bytes.Buffer
    if err := png.Encode(&buf, dc.Image()); err != nil { return nil, err }
    return buf.Bytes(), nil
}
```

### `internal/web/api/share_page.go`

```go
package api

import (
    "html/template"
    "net/http"
)

var shareTmpl = template.Must(template.New("share").Parse(`<!doctype html>
<html lang="ru">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>{{.Title}} — TIERL</title>
<meta name="description" content="{{.Description}}">
{{if .Unlisted}}<meta name="robots" content="noindex,nofollow">{{end}}

<meta property="og:type" content="website">
<meta property="og:site_name" content="TIERL">
<meta property="og:title" content="{{.Title}}">
<meta property="og:description" content="{{.Description}}">
<meta property="og:url" content="{{.CanonicalURL}}">
<meta property="og:image" content="{{.ImageURL}}">
<meta property="og:image:width" content="1200">
<meta property="og:image:height" content="630">
<meta property="og:image:type" content="image/png">
<meta property="og:locale" content="ru_RU">

<meta name="twitter:card" content="summary_large_image">
<meta name="twitter:title" content="{{.Title}}">
<meta name="twitter:description" content="{{.Description}}">
<meta name="twitter:image" content="{{.ImageURL}}">

<link rel="canonical" href="{{.CanonicalURL}}">
</head>
<body>
<h1>{{.Title}}</h1>
<p>{{.Description}}</p>
<a href="{{.CanonicalURL}}">Открыть в TIERL</a>
<script>/* SPA boot */</script>
</body>
</html>`))

func (s *Server) handleShareTerm(w http.ResponseWriter, r *http.Request) {
    slug := r.PathValue("slug")
    if !slugRe.MatchString(slug) { http.NotFound(w, r); return }
    t, err := s.store.GetTermBySlug(r.Context(), slug)
    if err != nil { http.NotFound(w, r); return }

    unlisted := t.Visibility != "public"
    setPageCacheHeaders(w, unlisted)

    base := s.publicBaseURL()
    _ = shareTmpl.Execute(w, map[string]any{
        "Title":        t.Title,
        "Description":  truncate(t.Definition, 180),
        "CanonicalURL": base + "/t/" + slug,
        "ImageURL":     base + "/og/t/" + slug + ".png",
        "Unlisted":     unlisted,
    })
}

func (s *Server) handleShareResult(w http.ResponseWriter, r *http.Request) {
    id := r.PathValue("id")
    if !idRe.MatchString(id) { http.NotFound(w, r); return }
    res, err := s.store.GetResult(r.Context(), id)
    if err != nil { http.NotFound(w, r); return }

    unlisted := res.Visibility != "public"
    setPageCacheHeaders(w, unlisted)

    base := s.publicBaseURL()
    _ = shareTmpl.Execute(w, map[string]any{
        "Title":        res.TermTitle + " — " + res.ScoreLabel,
        "Description":  res.DisplayName + " прошёл «" + res.TermTitle + "» в TIERL",
        "CanonicalURL": base + "/r/" + id,
        "ImageURL":     base + "/og/r/" + id + ".png",
        "Unlisted":     unlisted,
    })
}
```

### `internal/web/api/api.go` (фрагмент)

```go
func (s *Server) Routes() http.Handler {
    mux := http.NewServeMux()
    // ... существующие маршруты ...
    s.mountShareRoutes(mux)   // ← добавлено
    return s.middleware(mux)
}
```

### `internal/web/api/og_test.go` (фрагмент)

```go
func TestOGTermPNG(t *testing.T) {
    srv := newTestServer(t, seedTerm("go", "Go — язык программирования"))
    req := httptest.NewRequest("GET", "/og/t/go.png", nil)
    rec := httptest.NewRecorder()
    srv.ServeHTTP(rec, req)

    if rec.Code != 200 { t.Fatalf("code=%d", rec.Code) }
    if ct := rec.Header().Get("Content-Type"); ct != "image/png" {
        t.Fatalf("ct=%q", ct)
    }
    if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "s-maxage") {
        t.Fatalf("cache=%q", cc)
    }
    img, err := png.Decode(bytes.NewReader(rec.Body.Bytes()))
    if err != nil { t.Fatal(err) }
    b := img.Bounds()
    if b.Dx() != 1200 || b.Dy() != 630 {
        t.Fatalf("size=%dx%d", b.Dx(), b.Dy())
    }
}

func TestSharePageUnlistedNoindex(t *testing.T) {
    srv := newTestServer(t, seedTermUnlisted("secret", "hidden"))
    rec := httptest.NewRecorder()
    srv.ServeHTTP(rec, httptest.NewRequest("GET", "/t/secret", nil))
    body := rec.Body.String()
    if !strings.Contains(body, `name="robots" content="noindex,nofollow"`) {
        t.Fatal("missing noindex")
    }
    if !strings.Contains(rec.Header().Get("X-Robots-Tag"), "noindex") {
        t.Fatal("missing X-Robots-Tag")
    }
}

func TestSharePageOGTags(t *testing.T) {
    srv := newTestServer(t, seedResult("r1", "go", "gold"))
    rec := httptest.NewRecorder()
    srv.ServeHTTP(rec, httptest.NewRequest("GET", "/r/r1", nil))
    body := rec.Body.String()
    for _, want := range []string{
        `property="og:title"`,
        `property="og:image"`,
        `name="twitter:card"`,
        `content="summary_large_image"`,
        `/og/r/r1.png`,
    } {
        if !strings.Contains(body, want) { t.Errorf("missing %s", want) }
    }
}
```

### `web/src/pages/ShareResultPage.tsx` (фрагмент)

```tsx
import { useParams, Link } from "react-router-dom";
import { useEffect, useState } from "react";
import { apiGet } from "../lib/api"; // существующий helper

export default function ShareResultPage() {
  const { id } = useParams<{ id: string }>();
  const [res, setRes] = useState<any>(null);
  const [err, setErr] = useState(false);

  useEffect(() => {
    if (!id) return;
    apiGet(`/api/results/${id}`).then(setRes).catch(() => setErr(true));
  }, [id]);

  if (err) return <p>Результат не найден</p>;
  if (!res) return <p>Загрузка…</p>;

  return (
    <main>
      <h1>{res.termTitle} — {res.scoreLabel}</h1>
      <p>@{res.displayName}</p>
      <img src={`/og/r/${id}.png`} width={600} height={315}
           alt={`Превью результата ${res.termTitle}`} />
      <Link to="/">Играть</Link>
    </main>
  );
}
```

(Аналогично `ShareTermPage.tsx` для `/t/:slug`.)

### Маршруты `web/src/pages/routes.tsx`

```tsx
<Route path="/t/:slug" element={<ShareTermPage />} />
<Route path="/r/:id"   element={<ShareResultPage />} />
```

---

## Прогон проверок

```
$ go test ./...
ok  internal/web/api    0.42s

$ cd web && npm test -- --run && npm run build
 ✓ ShareTermPage.test.tsx (3)
 ✓ ShareResultPage.test.tsx (3)
 Test Files  2 passed
 Build success — dist/index.html (gzip: 42.1 kB)
```

---

## Соответствие критериям приёмки

| Критерий | Где проверяется |
|---|---|
| `og:title`/`og:image`/`twitter:card` на `/t` и `/r` | `share_page.go` шаблон + `TestSharePageOGTags` |
| PNG 1200×630 по URL | `/og/t/{slug}.png`, `/og/r/{id}.png`, `TestOGTermPNG` (проверяет `Bounds.Dx()==1200 && Dy()==630`) |
| Корректные cache headers | `setOGCacheHeaders` (`public, s-maxage=604800, immutable`) + `setPageCacheHeaders` (`public, s-maxage=3600` или `private, no-store` для unlisted) + ETag |
| Unlisted noindex | `<meta name="robots" content="noindex,nofollow">` + `X-Robots-Tag` + `TestSharePageUnlistedNoindex` |

## Запреты — соблюдены
- Миграции не тронуты.
- Секретов в коде нет (base-URL берётся из env через существующий `publicBaseURL()`).
- Правки только в `internal/web/static`, `internal/web/api`, `web/src/pages`.