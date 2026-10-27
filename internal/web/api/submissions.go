package api

import (
	"crypto/rand"
	"encoding/base64"
	"io"
	"net/http"
	"time"

	"github.com/basili4-1982/tierl/internal/auth"
	"github.com/basili4-1982/tierl/internal/tierlist"
)

// AnonCookieName is the anonymous participant cookie from ADR-002.
const AnonCookieName = "tierl_anon"

const anonCookieTTL = 365 * 24 * time.Hour

type placementDTO struct {
	ItemID string `json:"item_id"`
	TierID string `json:"tier_id"`
}

type submissionRequest struct {
	Placements []placementDTO `json:"placements"`
}

type submissionDTO struct {
	ID           string         `json:"id"`
	TierlistSlug string         `json:"tierlist_slug"`
	Placements   []placementDTO `json:"placements"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

func toPlacements(in []placementDTO) []tierlist.Placement {
	out := make([]tierlist.Placement, 0, len(in))
	for _, p := range in {
		out = append(out, tierlist.Placement{ItemID: p.ItemID, TierID: p.TierID})
	}
	return out
}

func fromPlacements(in []tierlist.Placement) []placementDTO {
	out := make([]placementDTO, 0, len(in))
	for _, p := range in {
		out = append(out, placementDTO{ItemID: p.ItemID, TierID: p.TierID})
	}
	return out
}

func toSubmissionDTO(d *tierlist.SubmissionDetails) submissionDTO {
	return submissionDTO{
		ID:           d.Submission.ID,
		TierlistSlug: d.Tierlist.Slug,
		Placements:   fromPlacements(d.Submission.Placements),
		CreatedAt:    d.Submission.CreatedAt.UTC(),
		UpdatedAt:    d.Submission.UpdatedAt.UTC(),
	}
}

// decodeSubmission reads the body and enforces the contract's minItems: 3
// before a session cookie is issued or any answer is written.
func decodeSubmission(w http.ResponseWriter, r *http.Request) (submissionRequest, bool) {
	var req submissionRequest
	if !decode(w, r, &req) {
		return req, false
	}
	if len(req.Placements) < tierlist.MinPlacements {
		writeError(w, http.StatusBadRequest, "bad_request", "Нужно разместить минимум 3 элемента", nil)
		return req, false
	}
	return req, true
}

func (a *API) createSubmission(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeSubmission(w, r)
	if !ok {
		return
	}
	sessionID, ok := a.anonSession(w, r, true)
	if !ok {
		return
	}
	d, err := a.tierlists.Submit(r.Context(), tierlist.SubmitInput{
		Slug:       r.PathValue("slug"),
		SessionID:  sessionID,
		Placements: toPlacements(req.Placements),
	}, false)
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, toSubmissionDTO(d))
}

func (a *API) replaceSubmission(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeSubmission(w, r)
	if !ok {
		return
	}
	sessionID, ok := a.anonSession(w, r, true)
	if !ok {
		return
	}
	d, err := a.tierlists.Submit(r.Context(), tierlist.SubmitInput{
		Slug:       r.PathValue("slug"),
		SessionID:  sessionID,
		Placements: toPlacements(req.Placements),
	}, true)
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toSubmissionDTO(d))
}

func (a *API) getMySubmission(w http.ResponseWriter, r *http.Request) {
	sessionID, ok := a.anonSession(w, r, false)
	if !ok {
		return
	}
	d, err := a.tierlists.MySubmission(r.Context(), r.PathValue("slug"), sessionID)
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toSubmissionDTO(d))
}

// anonSession returns the hashed anonymous session ID of the request. It
// issues a new cookie when create is true and the request has none.
func (a *API) anonSession(w http.ResponseWriter, r *http.Request, create bool) (string, bool) {
	token := ""
	if c, err := r.Cookie(AnonCookieName); err == nil {
		token = c.Value
	}
	if token == "" {
		if !create {
			return "", true
		}
		var b [32]byte
		if _, err := io.ReadFull(rand.Reader, b[:]); err != nil {
			a.log.ErrorContext(r.Context(), "anonymous session token", "error", err)
			writeError(w, http.StatusInternalServerError, "internal_error", "Внутренняя ошибка", nil)
			return "", false
		}
		token = base64.RawURLEncoding.EncodeToString(b[:])
		http.SetCookie(w, &http.Cookie{ //nolint:gosec // Secure вычисляется по схеме запроса
			Name:     AnonCookieName,
			Value:    token,
			Path:     "/",
			MaxAge:   int(anonCookieTTL / time.Second),
			Expires:  time.Now().Add(anonCookieTTL),
			HttpOnly: true,
			Secure:   secureRequest(r),
			SameSite: http.SameSiteLaxMode,
		})
	}
	return auth.HashToken(token), true
}
