package api

import (
	"net/http"

	"github.com/basili4-1982/tierl/internal/tierlist"
)

type resultDTO struct {
	Submission submissionDTO `json:"submission"`
	Tierlist   tierlistDTO   `json:"tierlist"`
}

type tierCountDTO struct {
	TierID  string  `json:"tier_id"`
	Count   int     `json:"count"`
	Percent float64 `json:"percent"`
}

type itemDistributionDTO struct {
	ItemID       string         `json:"item_id"`
	Label        string         `json:"label"`
	Distribution []tierCountDTO `json:"distribution"`
}

type similarDTO struct {
	Similarity float64 `json:"similarity"`
}

type resultStatsDTO struct {
	TotalSubmissions    int                   `json:"total_submissions"`
	InsufficientData    bool                  `json:"insufficient_data"`
	MinSubmissions      int                   `json:"min_submissions"`
	LikeYouPercent      *float64              `json:"like_you_percent"`
	SimilarityThreshold float64               `json:"similarity_threshold"`
	Items               []itemDistributionDTO `json:"items"`
	Similar             []similarDTO          `json:"similar"`
	Dissimilar          []similarDTO          `json:"dissimilar"`
}

func (a *API) getResult(w http.ResponseWriter, r *http.Request) {
	viewer, _ := a.auth.UserID(r)
	res, err := a.submissions.Result(r.Context(), r.PathValue("id"), viewer)
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	d := tierlist.Details{
		Tierlist: res.Tierlist, AuthorName: res.AuthorName,
		IsOwner: res.IsOwner, SubmissionsCount: res.SubmissionsCount,
	}
	writeJSON(w, http.StatusOK, resultDTO{
		Submission: toSubmissionDTO(&res.Submission),
		Tierlist:   toTierlistDTO(&d),
	})
}

func (a *API) getResultStats(w http.ResponseWriter, r *http.Request) {
	stats, err := a.submissions.Stats(r.Context(), r.PathValue("id"))
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	out := resultStatsDTO{
		TotalSubmissions: stats.TotalSubmissions, InsufficientData: stats.InsufficientData,
		MinSubmissions: stats.MinSubmissions, LikeYouPercent: stats.LikeYouPercent,
		SimilarityThreshold: stats.SimilarityThreshold,
		Items:               make([]itemDistributionDTO, 0, len(stats.Items)),
		Similar:             make([]similarDTO, 0, len(stats.Similar)),
		Dissimilar:          make([]similarDTO, 0, len(stats.Dissimilar)),
	}
	for _, item := range stats.Items {
		dto := itemDistributionDTO{ItemID: item.ItemID, Label: item.Label,
			Distribution: make([]tierCountDTO, 0, len(item.Counts))}
		for _, c := range item.Counts {
			dto.Distribution = append(dto.Distribution, tierCountDTO{TierID: c.TierID, Count: c.Count, Percent: c.Percent})
		}
		out.Items = append(out.Items, dto)
	}
	for _, s := range stats.Similar {
		out.Similar = append(out.Similar, similarDTO{Similarity: s.Similarity})
	}
	for _, s := range stats.Dissimilar {
		out.Dissimilar = append(out.Dissimilar, similarDTO{Similarity: s.Similarity})
	}
	writeJSON(w, http.StatusOK, out)
}
