package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/basili4-1982/tierl/internal/anonsession"
	"github.com/basili4-1982/tierl/internal/submission"
	"github.com/basili4-1982/tierl/internal/tierlist"
)

type placementDTO struct {
	ItemID string `json:"item_id"`
	TierID string `json:"tier_id"`
}

type submissionInputDTO struct {
	Placements []placementDTO `json:"placements"`
}

type submissionDTO struct {
	ID           string         `json:"id"`
	TierlistSlug string         `json:"tierlist_slug"`
	Placements   []placementDTO `json:"placements"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

type submissionPageDTO struct {
	Items      []submissionDTO `json:"items"`
	NextCursor *string         `json:"next_cursor"`
}

func toSubmissionDTO(s *submission.Submission) submissionDTO {
	out := submissionDTO{
		ID: s.ID, TierlistSlug: s.TierlistSlug,
		Placements: make([]placementDTO, 0, len(s.Placements)),
		CreatedAt:  s.CreatedAt.UTC(), UpdatedAt: s.UpdatedAt.UTC(),
	}
	for _, p := range s.Placements {
		out.Placements = append(out.Placements, placementDTO{ItemID: p.ItemID, TierID: p.TierID})
	}
	return out
}

func (in submissionInputDTO) placements() []tierlist.Placement {
	out := make([]tierlist.Placement, 0, len(in.Placements))
	for _, p := range in.Placements {
		out = append(out, tierlist.Placement{ItemID: p.ItemID, TierID: p.TierID})
	}
	return out
}

// submissionSession returns the anonymous session for a changing request and
// writes 403 when CSRF fails. A missing cookie yields a fresh session that is
// sent to the client only after a successful write.
func (a *API) submissionSession(w http.ResponseWriter, r *http.Request) (anonsession.Session, bool) {
	if !csrfOK(r) {
		writeError(w, http.StatusForbidden, "csrf_failed", "Неверный CSRF-токен", nil)
		return anonsession.Session{}, false
	}
	s, err := anonsession.Ensure(r, nil)
	if err != nil {
		a.writeServiceError(w, r, err)
		return anonsession.Session{}, false
	}
	return s, true
}

func (a *API) createSubmission(w http.ResponseWriter, r *http.Request) {
	session, ok := a.submissionSession(w, r)
	if !ok {
		return
	}
	var req submissionInputDTO
	if !decode(w, r, &req) {
		return
	}
	sub, err := a.submissions.Create(r.Context(), submission.SubmitInput{
		Slug: r.PathValue("slug"), SessionID: session.Hash, Placements: req.placements(),
	})
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	if session.Fresh {
		anonsession.SetCookie(w, session)
	}
	writeJSON(w, http.StatusCreated, toSubmissionDTO(sub))
}

func (a *API) replaceMySubmission(w http.ResponseWriter, r *http.Request) {
	session, ok := a.submissionSession(w, r)
	if !ok {
		return
	}
	var req submissionInputDTO
	if !decode(w, r, &req) {
		return
	}
	sub, err := a.submissions.Replace(r.Context(), submission.SubmitInput{
		Slug: r.PathValue("slug"), SessionID: session.Hash, Placements: req.placements(),
	})
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	if session.Fresh {
		anonsession.SetCookie(w, session)
	}
	writeJSON(w, http.StatusOK, toSubmissionDTO(sub))
}

func (a *API) getMySubmission(w http.ResponseWriter, r *http.Request) {
	session, ok := anonsession.FromRequest(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "Не найдено", nil)
		return
	}
	sub, err := a.submissions.Mine(r.Context(), r.PathValue("slug"), session.Hash)
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toSubmissionDTO(sub))
}

// listMySubmissions returns the anonymous session's answers. A guest without
// the anon cookie simply has none; this endpoint never reads a user session.
func (a *API) listMySubmissions(w http.ResponseWriter, r *http.Request) {
	session, ok := anonsession.FromRequest(r)
	if !ok {
		writeJSON(w, http.StatusOK, submissionPageDTO{Items: []submissionDTO{}})
		return
	}
	subs, err := a.submissions.ListMine(r.Context(), session.Hash)
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	items := make([]submissionDTO, 0, len(subs))
	for i := range subs {
		items = append(items, toSubmissionDTO(&subs[i]))
	}
	writeJSON(w, http.StatusOK, submissionPageDTO{Items: items})
}

// listUserSubmissions is the authenticated "my answers" endpoint. Answers are
// deliberately not linked to accounts (ADR-002), so until the claim flow lands
// there is nothing to list; the endpoint exists to keep /me authenticated and
// to prove that tierl_anon is not an authentication credential.
func (a *API) listUserSubmissions(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireUser(w, r); !ok {
		return
	}
	writeJSON(w, http.StatusOK, submissionPageDTO{Items: []submissionDTO{}})
}

// writeSubmissionError maps the submission-specific errors that are not part
// of the tierlist service.
func (a *API) writeSubmissionError(w http.ResponseWriter, r *http.Request, err error) bool {
	var already *submission.AlreadySubmittedError
	switch {
	case errors.Is(err, submission.ErrTooFewPlacements):
		writeError(w, http.StatusBadRequest, "too_few_placements",
			"Разместите не меньше 3 элементов", map[string]any{"min_placements": submission.MinPlacements})
		return true
	case errors.As(err, &already):
		details := map[string]any{}
		if already.ResultID != "" {
			details["result_id"] = already.ResultID
		}
		writeError(w, http.StatusConflict, "already_submitted", "Вы уже отвечали на этот опрос", details)
		return true
	case errors.Is(err, submission.ErrAlreadySubmitted):
		writeError(w, http.StatusConflict, "already_submitted", "Вы уже отвечали на этот опрос", nil)
		return true
	case errors.Is(err, tierlist.ErrNotAcceptingSubmissions):
		writeError(w, http.StatusConflict, "tierlist_closed", "Опрос закрыт", nil)
		return true
	}
	return false
}
