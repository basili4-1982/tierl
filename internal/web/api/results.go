package api

import (
	"net/http"

	"code.sarnas.ru/tierl/internal/tierlist"
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

type similarResultDTO struct {
	ResultID   string  `json:"result_id"`
	Similarity float64 `json:"similarity"`
}

type resultStatsDTO struct {
	TotalSubmissions    int                   `json:"total_submissions"`
	InsufficientData    bool                  `json:"insufficient_data"`
	MinSubmissions      int                   `json:"min_submissions"`
	LikeYouPercent      *float64              `json:"like_you_percent"`
	SimilarityThreshold float64               `json:"similarity_threshold"`
	Items               []itemDistributionDTO `json:"items"`
	Similar             []similarResultDTO    `json:"similar"`
	Dissimilar          []similarResultDTO    `json:"dissimilar"`
}

func toTierlistDetails(d *tierlist.SubmissionDetails) *tierlist.Details {
	return &tierlist.Details{
		Tierlist:         d.Tierlist,
		AuthorName:       d.AuthorName,
		IsOwner:          d.IsOwner,
		SubmissionsCount: d.SubmissionsCount,
	}
}

func (a *API) getResult(w http.ResponseWriter, r *http.Request) {
	viewer, _ := a.auth.UserID(r)
	d, err := a.tierlists.Result(r.Context(), r.PathValue("id"), viewer)
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, resultDTO{
		Submission: toSubmissionDTO(d),
		Tierlist:   toTierlistDTO(toTierlistDetails(d)),
	})
}

// toResultStatsDTO maps the anonymous service statistics to the wire format.
// It deliberately copies only public aggregate fields: session and user
// identifiers never enter the response.
func toResultStatsDTO(stats *tierlist.ResultStats) resultStatsDTO {
	out := resultStatsDTO{
		TotalSubmissions:    stats.TotalSubmissions,
		InsufficientData:    stats.InsufficientData,
		MinSubmissions:      stats.MinSubmissions,
		LikeYouPercent:      stats.LikeYouPercent,
		SimilarityThreshold: stats.SimilarityThreshold,
		Items:               make([]itemDistributionDTO, 0, len(stats.Items)),
		Similar:             make([]similarResultDTO, 0, len(stats.Similar)),
		Dissimilar:          make([]similarResultDTO, 0, len(stats.Dissimilar)),
	}
	for _, item := range stats.Items {
		dto := itemDistributionDTO{
			ItemID:       item.ItemID,
			Label:        item.Label,
			Distribution: make([]tierCountDTO, 0, len(item.Distribution)),
		}
		for _, tier := range item.Distribution {
			dto.Distribution = append(dto.Distribution, tierCountDTO{
				TierID: tier.TierID, Count: tier.Count, Percent: tier.Percent,
			})
		}
		out.Items = append(out.Items, dto)
	}
	for _, item := range stats.Similar {
		out.Similar = append(out.Similar, similarResultDTO{ResultID: item.ResultID, Similarity: item.Similarity})
	}
	for _, item := range stats.Dissimilar {
		out.Dissimilar = append(out.Dissimilar, similarResultDTO{ResultID: item.ResultID, Similarity: item.Similarity})
	}
	return out
}

func (a *API) getResultStats(w http.ResponseWriter, r *http.Request) {
	stats, err := a.tierlists.Stats(r.Context(), r.PathValue("id"))
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toResultStatsDTO(stats))
}
