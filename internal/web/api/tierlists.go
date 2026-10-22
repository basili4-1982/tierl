package api

import (
	"net/http"
	"time"

	"github.com/basili4-1982/tierl/internal/domain"
	"github.com/basili4-1982/tierl/internal/tierlist"
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
	Tiers       *[]tierInputDTO `json:"tiers"`
	Items       *[]itemInputDTO `json:"items"`
}

func (p patchRequest) empty() bool {
	return p.Title == nil && p.Description == nil && p.Visibility == nil && p.Tiers == nil && p.Items == nil
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

func (a *API) createTierlist(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	var req createRequest
	if !decode(w, r, &req) {
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
	if req.Tiers != nil {
		in.Tiers = toTierInputs(*req.Tiers)
	}
	if req.Items != nil {
		in.Items = toItemInputs(*req.Items)
	}
	d, err := a.tierlists.Update(r.Context(), userID, r.PathValue("slug"), in)
	if err != nil {
		a.writeServiceError(w, r, err)
		return
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
	if err := a.tierlists.Delete(r.Context(), userID, r.PathValue("slug")); err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
