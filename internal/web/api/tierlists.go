package api

import (
	"net/http"
	"strings"
	"time"

	"code.sarnas.ru/tierl/internal/domain"
	"code.sarnas.ru/tierl/internal/tierlist"
)

type tierInputDTO struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

type itemInputDTO struct {
	Label    string  `json:"label"`
	ImageURL *string `json:"image_url"`
}

type createRequest struct {
	Title       string         `json:"title"`
	Description string         `json:"description"`
	Visibility  string         `json:"visibility"`
	Slug        string         `json:"slug"`
	Tiers       []tierInputDTO `json:"tiers"`
	Items       []itemInputDTO `json:"items"`
}

type patchRequest struct {
	Title       *string         `json:"title"`
	Description *string         `json:"description"`
	Visibility  *string         `json:"visibility"`
	Status      *string         `json:"status"`
	Tiers       *[]tierInputDTO `json:"tiers"`
	Items       *[]itemInputDTO `json:"items"`
}

func (p patchRequest) empty() bool {
	return p.Title == nil && p.Description == nil && p.Visibility == nil &&
		p.Status == nil && p.Tiers == nil && p.Items == nil
}

// parseStatus accepts the lifecycle names and "public" as the author-facing
// synonym of reopening a tier list.
func parseStatus(raw string) (domain.Status, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "closed":
		return domain.StatusClosed, true
	case "open", "public":
		return domain.StatusOpen, true
	default:
		return "", false
	}
}

type tierDTO struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Color    string `json:"color"`
	Position int    `json:"position"`
}

type itemDTO struct {
	ID       string  `json:"id"`
	Label    string  `json:"label"`
	ImageURL *string `json:"image_url"`
	Position int     `json:"position"`
}

type authorDTO struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
}

type tierlistDTO struct {
	ID               string     `json:"id"`
	Slug             string     `json:"slug"`
	Title            string     `json:"title"`
	Description      string     `json:"description"`
	Visibility       string     `json:"visibility"`
	Status           string     `json:"status"`
	Tiers            []tierDTO  `json:"tiers"`
	Items            []itemDTO  `json:"items"`
	Author           authorDTO  `json:"author"`
	IsOwner          bool       `json:"is_owner"`
	SubmissionsCount int        `json:"submissions_count"`
	CreatedAt        time.Time  `json:"created_at"`
	ClosedAt         *time.Time `json:"closed_at"`
}

func toTierlistDTO(d *tierlist.Details) tierlistDTO {
	t := d.Tierlist
	out := tierlistDTO{
		ID:               t.ID,
		Slug:             t.Slug,
		Title:            t.Title,
		Description:      t.Description,
		Visibility:       string(t.Visibility),
		Status:           string(t.Status),
		Tiers:            make([]tierDTO, 0, len(t.Tiers)),
		Items:            make([]itemDTO, 0, len(t.Items)),
		Author:           authorDTO{ID: t.AuthorID, DisplayName: d.AuthorName},
		IsOwner:          d.IsOwner,
		SubmissionsCount: d.SubmissionsCount,
		CreatedAt:        t.CreatedAt.UTC(),
		ClosedAt:         d.ClosedAt(),
	}
	if out.ClosedAt != nil {
		c := out.ClosedAt.UTC()
		out.ClosedAt = &c
	}
	for _, tr := range t.Tiers {
		out.Tiers = append(out.Tiers, tierDTO{ID: tr.ID, Name: tr.Name, Color: tr.Color, Position: tr.Position})
	}
	for _, it := range t.Items {
		dto := itemDTO{ID: it.ID, Label: it.Label, Position: it.Position}
		if it.ImageURL != "" {
			u := it.ImageURL
			dto.ImageURL = &u
		}
		out.Items = append(out.Items, dto)
	}
	return out
}

type myTierlistDTO struct {
	ID               string    `json:"id"`
	Slug             string    `json:"slug"`
	Title            string    `json:"title"`
	Description      string    `json:"description"`
	Visibility       string    `json:"visibility"`
	Status           string    `json:"status"`
	ItemsCount       int       `json:"items_count"`
	SubmissionsCount int       `json:"submissions_count"`
	CoverURL         *string   `json:"cover_url"`
	Author           authorDTO `json:"author"`
	CreatedAt        time.Time `json:"created_at"`
}

type myTierlistsDTO struct {
	Items      []myTierlistDTO `json:"items"`
	NextCursor *string         `json:"next_cursor"`
}

func toMyTierlistDTO(m tierlist.MyTierlist) myTierlistDTO {
	return myTierlistDTO{
		ID:               m.ID,
		Slug:             m.Slug,
		Title:            m.Title,
		Description:      m.Description,
		Visibility:       string(m.Visibility),
		Status:           string(m.Status),
		ItemsCount:       m.ItemCount,
		SubmissionsCount: m.SubmissionsCount,
		Author:           authorDTO{ID: m.AuthorID, DisplayName: m.AuthorName},
		CreatedAt:        m.CreatedAt.UTC(),
	}
}

func toTierInputs(in []tierInputDTO) []tierlist.TierInput {
	out := make([]tierlist.TierInput, 0, len(in))
	for _, t := range in {
		out = append(out, tierlist.TierInput{Name: t.Name, Color: t.Color})
	}
	return out
}

func toItemInputs(in []itemInputDTO) []tierlist.ItemInput {
	out := make([]tierlist.ItemInput, 0, len(in))
	for _, it := range in {
		item := tierlist.ItemInput{Label: it.Label}
		if it.ImageURL != nil {
			item.ImageURL = *it.ImageURL
		}
		out = append(out, item)
	}
	return out
}

func (a *API) listMyTierlists(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	entries, err := a.tierlists.ListMine(r.Context(), userID)
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	items := make([]myTierlistDTO, 0, len(entries))
	for _, e := range entries {
		items = append(items, toMyTierlistDTO(e))
	}
	writeJSON(w, http.StatusOK, myTierlistsDTO{Items: items})
}

func (a *API) createTierlist(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	var req createRequest
	if !decode(w, r, &req) {
		return
	}
	if !a.validateItemImages(w, userID, req.Items) {
		return
	}
	d, err := a.tierlists.Create(r.Context(), userID, tierlist.CreateInput{
		Title:       req.Title,
		Description: req.Description,
		Visibility:  domain.Visibility(req.Visibility),
		Slug:        req.Slug,
		Tiers:       toTierInputs(req.Tiers),
		Items:       toItemInputs(req.Items),
	})
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, toTierlistDTO(d))
}

func (a *API) getTierlist(w http.ResponseWriter, r *http.Request) {
	viewer, _ := a.auth.UserID(r)
	d, err := a.tierlists.Get(r.Context(), viewer, r.PathValue("slug"))
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toTierlistDTO(d))
}

func (a *API) updateTierlist(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	var req patchRequest
	if !decode(w, r, &req) {
		return
	}
	if req.empty() {
		writeError(w, http.StatusBadRequest, "bad_request", "Некорректное тело запроса", nil)
		return
	}
	in := tierlist.UpdateInput{Title: req.Title, Description: req.Description}
	if req.Visibility != nil {
		v := domain.Visibility(*req.Visibility)
		in.Visibility = &v
	}
	if req.Status != nil {
		status, valid := parseStatus(*req.Status)
		if !valid {
			writeError(w, http.StatusBadRequest, "bad_request", "Некорректный статус опроса", nil)
			return
		}
		in.Status = &status
	}
	if req.Tiers != nil {
		in.Tiers = toTierInputs(*req.Tiers)
	}
	if req.Items != nil {
		if !a.validateItemImages(w, userID, *req.Items) {
			return
		}
		in.Items = toItemInputs(*req.Items)
	}

	var oldItems []domain.Item
	if req.Items != nil && a.media != nil {
		if old, err := a.tierlists.Get(r.Context(), userID, r.PathValue("slug")); err == nil {
			oldItems = old.Tierlist.Items
		}
	}
	d, err := a.tierlists.Update(r.Context(), userID, r.PathValue("slug"), in)
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	if req.Items != nil {
		a.deleteRemovedImages(r.Context(), userID, oldItems, d.Tierlist.Items)
	}
	writeJSON(w, http.StatusOK, toTierlistDTO(d))
}

func (a *API) closeTierlist(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	d, err := a.tierlists.Close(r.Context(), userID, r.PathValue("slug"))
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toTierlistDTO(d))
}

func (a *API) deleteTierlist(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	var images []string
	if a.media != nil {
		if old, err := a.tierlists.Get(r.Context(), userID, r.PathValue("slug")); err == nil {
			images = itemImageURLs(old.Tierlist.Items)
		}
	}
	if err := a.tierlists.Delete(r.Context(), userID, r.PathValue("slug")); err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	a.deleteImages(r.Context(), userID, images)
	w.WriteHeader(http.StatusNoContent)
}
