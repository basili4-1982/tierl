package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"html"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"code.sarnas.ru/tierl/internal/domain"
	"code.sarnas.ru/tierl/internal/tierlist"
	"code.sarnas.ru/tierl/internal/web/static"
)

var (
	slugRe  = regexp.MustCompile(`^[A-Za-z0-9_-]{3,64}$`)
	idRe    = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
	titleRe = regexp.MustCompile(`(?is)<title[^>]*>.*?</title>`)
)

// OGResult is the data needed to render a result page and its preview.
type OGResult struct {
	ID         string
	Tierlist   *domain.Tierlist
	AuthorName string
	Placements []tierlist.Placement
	UpdatedAt  *time.Time
}

// ResultProvider resolves a result by its public ID.
type ResultProvider interface {
	GetOGResult(ctx context.Context, id string) (*OGResult, error)
}

type shareData struct {
	Title        string
	Description  string
	CanonicalURL string
	ImageURL     string
	Unlisted     bool
}

// ShareHandler exposes the root-level share and OG routes. The web handler
// mounts it at /t/, /r/ and /og/ so the SPA fallback is left intact.
func (a *API) ShareHandler() http.Handler { return a.mux }

// mountShareRoutes registers the pages and images on the API mux. Both root
// paths and API-prefixed paths are available: the former for browsers, the
// latter for clients that only know api.Prefix.
func (a *API) mountShareRoutes() {
	a.mux.HandleFunc("GET /t/{slug}", a.handleShareTerm)
	a.mux.HandleFunc("GET /r/{id}", a.handleShareResult)
	a.mux.HandleFunc("GET /og/t/{name}", a.handleOGTerm)
	a.mux.HandleFunc("GET /og/r/{name}", a.handleOGResult)
	a.mux.HandleFunc("GET "+Prefix+"/og/t/{name}", a.handleOGTerm)
	a.mux.HandleFunc("GET "+Prefix+"/og/r/{name}", a.handleOGResult)
	a.mux.HandleFunc("GET /t/{slug}/og.png", a.handleOGTerm)
	a.mux.HandleFunc("GET /r/{id}/og.png", a.handleOGResult)
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

func (a *API) handleOGTerm(w http.ResponseWriter, r *http.Request) {
	slug := strings.TrimSuffix(r.PathValue("slug"), ".png")
	name := strings.TrimSuffix(r.PathValue("name"), ".png")
	if slug == "" {
		slug = name
	}
	if !slugRe.MatchString(slug) {
		http.NotFound(w, r)
		return
	}
	t, err := a.readableTierlist(r.Context(), slug)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	png, err := renderOG(OGSpec{
		Kind: "term", Title: t.Title, Subtitle: t.Description,
		Author: a.authorName(r.Context(), t.AuthorID),
	})
	if err != nil {
		http.Error(w, "render", http.StatusInternalServerError)
		return
	}

	setOGCacheHeadersFor(w, t.Visibility != domain.VisibilityPublic, etag(t.UpdatedAt, t.ID))
	w.Header().Set("Content-Length", strconv.Itoa(len(png)))
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(png)
}

func (a *API) handleOGResult(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSuffix(r.PathValue("id"), ".png")
	name := strings.TrimSuffix(r.PathValue("name"), ".png")
	if id == "" {
		id = name
	}
	if !idRe.MatchString(id) {
		http.NotFound(w, r)
		return
	}
	res, err := a.resultFor(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	t := res.Tierlist

	png, err := renderOG(OGSpec{
		Kind: "result", Title: t.Title, Subtitle: t.Description,
		Author: res.AuthorName, Tiers: t.Tiers, Items: t.Items, Placements: res.Placements,
	})
	if err != nil {
		http.Error(w, "render", http.StatusInternalServerError)
		return
	}

	updated := t.UpdatedAt
	if res.UpdatedAt != nil {
		updated = *res.UpdatedAt
	}
	setOGCacheHeadersFor(w, t.Visibility != domain.VisibilityPublic, etag(updated, id))
	w.Header().Set("Content-Length", strconv.Itoa(len(png)))
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(png)
}

func (a *API) handleShareTerm(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	if !slugRe.MatchString(slug) {
		http.NotFound(w, r)
		return
	}
	t, err := a.readableTierlist(r.Context(), slug)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	base := publicBaseURL(r)
	desc := t.Description
	if strings.TrimSpace(desc) == "" {
		desc = "Тир-лист «" + t.Title + "» на TIERL"
	}
	d := shareData{
		Title:        t.Title,
		Description:  truncate(desc, 180),
		CanonicalURL: base + "/t/" + slug,
		ImageURL:     base + "/og/t/" + slug + ".png",
		Unlisted:     t.Visibility != domain.VisibilityPublic,
	}
	a.writeSharePage(w, r, d)
}

func (a *API) handleShareResult(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !idRe.MatchString(id) {
		http.NotFound(w, r)
		return
	}
	res, err := a.resultFor(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	t := res.Tierlist

	base := publicBaseURL(r)
	desc := "Результат «" + t.Title + "» в TIERL"
	if res.AuthorName != "" {
		desc = res.AuthorName + " прошёл «" + t.Title + "» в TIERL"
	}
	d := shareData{
		Title:        t.Title + " — результат",
		Description:  truncate(desc, 180),
		CanonicalURL: base + "/r/" + id,
		ImageURL:     base + "/og/r/" + id + ".png",
		Unlisted:     t.Visibility != domain.VisibilityPublic,
	}
	a.writeSharePage(w, r, d)
}

func (a *API) writeSharePage(w http.ResponseWriter, r *http.Request, d shareData) {
	setPageCacheHeaders(w, d.Unlisted)
	doc := shareHTML(d)
	w.Header().Set("Content-Length", strconv.Itoa(len(doc)))
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(doc)
}

func (a *API) readableTierlist(ctx context.Context, slug string) (*domain.Tierlist, error) {
	if a.tierlists != nil {
		d, err := a.tierlists.Get(ctx, "", slug)
		if err != nil {
			return nil, err
		}
		return d.Tierlist, nil
	}
	if a.store != nil {
		t, err := a.store.GetBySlug(ctx, slug)
		if err != nil {
			return nil, err
		}
		if !readableStatus(t.Status) {
			return nil, tierlist.ErrNotFound
		}
		return t, nil
	}
	return nil, tierlist.ErrNotFound
}

func (a *API) resultFor(ctx context.Context, id string) (*OGResult, error) {
	if a.results != nil {
		res, err := a.results.GetOGResult(ctx, id)
		if err != nil {
			return nil, err
		}
		if res == nil || res.Tierlist == nil || !readableStatus(res.Tierlist.Status) {
			return nil, tierlist.ErrNotFound
		}
		return res, nil
	}
	if a.store != nil {
		t, err := a.store.GetByID(ctx, id)
		if err != nil {
			return nil, err
		}
		if !readableStatus(t.Status) {
			return nil, tierlist.ErrNotFound
		}
		return &OGResult{ID: id, Tierlist: t}, nil
	}
	// Without a data source the result page is still useful as a share shell;
	// production wiring always provides ResultProvider.
	return &OGResult{ID: id, Tierlist: &domain.Tierlist{
		ID: id, Title: "Результат", Visibility: domain.VisibilityPublic, Status: domain.StatusOpen,
	}}, nil
}

func (a *API) authorName(ctx context.Context, id string) string {
	if a.users == nil {
		return ""
	}
	prof, err := a.users.Profile(ctx, id)
	if err != nil {
		return ""
	}
	return prof.DisplayName
}

func readableStatus(s domain.Status) bool {
	return s == domain.StatusOpen || s == domain.StatusClosed
}

func publicBaseURL(r *http.Request) string {
	scheme := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto"))
	if scheme == "" {
		if r.TLS != nil {
			scheme = "https"
		} else {
			scheme = "http"
		}
	}
	host := strings.TrimSpace(r.Header.Get("X-Forwarded-Host"))
	if host == "" {
		host = r.Host
	}
	if host == "" {
		host = "localhost"
	}
	return scheme + "://" + strings.TrimRight(host, "/")
}

func setOGCacheHeadersFor(w http.ResponseWriter, unlisted bool, tag string) {
	setOGCacheHeaders(w)
	if unlisted {
		w.Header().Set("Cache-Control", "private, no-store")
		w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	}
	if tag != "" {
		w.Header().Set("ETag", tag)
	}
}

func etag(t time.Time, fallback string) string {
	key := fallback
	if !t.IsZero() {
		key = t.UTC().Format(time.RFC3339Nano) + ":" + fallback
	}
	sum := sha256.Sum256([]byte(key))
	return `"` + hex.EncodeToString(sum[:8]) + `"`
}

func shareHTML(d shareData) []byte {
	head := metaTags(d)
	if doc, err := static.Index(static.FS()); err == nil {
		doc = setDocumentTitle(doc, d.Title+" — TIERL")
		return static.InjectHead(doc, head)
	}
	return []byte(standaloneShareHTML(head, d))
}

func setDocumentTitle(doc []byte, title string) []byte {
	repl := "<title>" + html.EscapeString(title) + "</title>"
	if titleRe.Match(doc) {
		return titleRe.ReplaceAllFunc(doc, func([]byte) []byte { return []byte(repl) })
	}
	return static.InjectHead(doc, repl)
}

func metaTags(d shareData) string {
	var b strings.Builder
	esc := html.EscapeString
	if d.Unlisted {
		b.WriteString(`<meta name="robots" content="noindex,nofollow">` + "\n")
	}
	fmt.Fprintf(&b, `<meta name="description" content="%s">`+"\n", esc(d.Description))
	b.WriteString(`<meta property="og:type" content="website">` + "\n")
	b.WriteString(`<meta property="og:site_name" content="TIERL">` + "\n")
	fmt.Fprintf(&b, `<meta property="og:title" content="%s">`+"\n", esc(d.Title))
	fmt.Fprintf(&b, `<meta property="og:description" content="%s">`+"\n", esc(d.Description))
	fmt.Fprintf(&b, `<meta property="og:url" content="%s">`+"\n", esc(d.CanonicalURL))
	fmt.Fprintf(&b, `<meta property="og:image" content="%s">`+"\n", esc(d.ImageURL))
	b.WriteString(`<meta property="og:image:width" content="1200">` + "\n")
	b.WriteString(`<meta property="og:image:height" content="630">` + "\n")
	b.WriteString(`<meta property="og:image:type" content="image/png">` + "\n")
	b.WriteString(`<meta property="og:locale" content="ru_RU">` + "\n")
	b.WriteString(`<meta name="twitter:card" content="summary_large_image">` + "\n")
	fmt.Fprintf(&b, `<meta name="twitter:title" content="%s">`+"\n", esc(d.Title))
	fmt.Fprintf(&b, `<meta name="twitter:description" content="%s">`+"\n", esc(d.Description))
	fmt.Fprintf(&b, `<meta name="twitter:image" content="%s">`+"\n", esc(d.ImageURL))
	fmt.Fprintf(&b, `<link rel="canonical" href="%s">`+"\n", esc(d.CanonicalURL))
	return b.String()
}

func standaloneShareHTML(head string, d shareData) string {
	esc := html.EscapeString
	return `<!doctype html>
<html lang="ru">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>` + esc(d.Title) + ` — TIERL</title>
` + head + `</head>
<body>
<main>
<h1>` + esc(d.Title) + `</h1>
<p>` + esc(d.Description) + `</p>
<p><img src="` + esc(d.ImageURL) + `" width="600" height="315" alt="Превью"></p>
<p><a href="` + esc(d.CanonicalURL) + `">Открыть в TIERL</a></p>
</main>
</body>
</html>`
}
