package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"code.sarnas.ru/tierl/internal/tierlist"
)

// reportRequest is the body of POST /tierlists/{slug}/reports. The optional
// reporter fields are accepted for clients that already have a stable
// identifier; otherwise the signed-in account or anonymous session is used.
type reportRequest struct {
	Reason   string `json:"reason"`
	Comment  string `json:"comment"`
	Reporter string `json:"reporter,omitempty"`
}

type reportDTO struct {
	ID           string    `json:"id"`
	TierlistID   string    `json:"tierlist_id"`
	TierlistSlug string    `json:"tierlist_slug"`
	Reason       string    `json:"reason"`
	Comment      string    `json:"comment,omitempty"`
	ReportsCount int       `json:"reports_count"`
	Hidden       bool      `json:"hidden"`
	CreatedAt    time.Time `json:"created_at"`
}

// decodeReport accepts an empty body (the reason then defaults to "other") and
// ignores unknown fields so older/newer clients stay compatible.
func decodeReport(w http.ResponseWriter, r *http.Request) (reportRequest, bool) {
	var req reportRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err := dec.Decode(&req); err != nil {
		if errors.Is(err, io.EOF) {
			return req, true
		}
		writeError(w, http.StatusBadRequest, "bad_request", "Некорректное тело запроса", nil)
		return req, false
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "bad_request", "Некорректное тело запроса", nil)
		return req, false
	}
	return req, true
}

// reporterKey returns a stable non-reversible identifier of the reporter.
// Signed-in users are identified by account, guests by the anonymous session
// cookie (issued on the first report).
func (a *API) reporterKey(w http.ResponseWriter, r *http.Request, fallback string) (string, bool) {
	if id, ok := a.auth.UserID(r); ok && id != "" {
		return "user:" + id, true
	}
	if fallback != "" {
		return "client:" + fallback, true
	}
	session, ok := a.anonSession(w, r, true)
	if !ok {
		return "", false
	}
	return "anon:" + session, true
}

func (a *API) createReport(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeReport(w, r)
	if !ok {
		return
	}
	key, ok := a.reporterKey(w, r, req.Reporter)
	if !ok {
		return
	}
	res, err := a.tierlists.Report(r.Context(), tierlist.ReportInput{
		Slug:        r.PathValue("slug"),
		ReporterKey: key,
		Reason:      req.Reason,
		Comment:     req.Comment,
	})
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, reportDTO{
		ID:           res.Report.ID,
		TierlistID:   res.Report.TierlistID,
		TierlistSlug: r.PathValue("slug"),
		Reason:       res.Report.Reason,
		Comment:      res.Report.Comment,
		ReportsCount: res.ReportsCount,
		Hidden:       res.Hidden,
		CreatedAt:    res.Report.CreatedAt.UTC(),
	})
}
