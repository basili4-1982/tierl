package tierlist

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/basili4-1982/tierl/internal/domain"
	"github.com/basili4-1982/tierl/internal/similarity"
)

// MinPlacements is the minimal number of items a participant must place.
const MinPlacements = domain.MinItems

// ErrAlreadySubmitted is returned when a session posts a second answer.
var ErrAlreadySubmitted = errors.New("tierlist: already submitted")

// AlreadySubmittedError reports the existing result of the session.
type AlreadySubmittedError struct {
	ResultID string
}

// Error implements error.
func (e *AlreadySubmittedError) Error() string {
	return fmt.Sprintf("tierlist: already submitted as %s", e.ResultID)
}

// Unwrap makes errors.Is(err, ErrAlreadySubmitted) succeed.
func (e *AlreadySubmittedError) Unwrap() error { return ErrAlreadySubmitted }

// SubmissionRecord is a stored answer together with its timestamps.
type SubmissionRecord struct {
	ID         string
	TierlistID string
	SessionID  string
	Placements []Placement
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// SubmissionLookup reads answers by their public ID or by session. It is
// optional: a Store that also acts as an Answers source may implement it.
type SubmissionLookup interface {
	// SubmissionByID returns the answer with the given result ID or ErrNotFound.
	SubmissionByID(ctx context.Context, id string) (*SubmissionRecord, error)
	// SubmissionBySession returns the answer of a session on a tier list or
	// ErrNotFound.
	SubmissionBySession(ctx context.Context, tierlistID, sessionID string) (*SubmissionRecord, error)
}

// SubmissionDetails is a participant result prepared for the HTTP layer.
type SubmissionDetails struct {
	Submission       SubmissionRecord
	Tierlist         *domain.Tierlist
	AuthorName       string
	IsOwner          bool
	SubmissionsCount int
}

// SubmitInput is the input of Service.Submit.
type SubmitInput struct {
	Slug       string
	SessionID  string
	Placements []Placement
}

// Submit stores an answer of a session. With replace=false an existing answer
// is reported as AlreadySubmittedError; with replace=true it is overwritten
// and keeps its result ID.
func (s *Service) Submit(ctx context.Context, in SubmitInput, replace bool) (*SubmissionDetails, error) {
	if in.SessionID == "" {
		return nil, &domain.ValidationError{Field: "session", Reason: "must not be empty"}
	}
	if len(in.Placements) < MinPlacements {
		return nil, &domain.ValidationError{
			Field:  "placements",
			Reason: fmt.Sprintf("count %d must be at least %d", len(in.Placements), MinPlacements),
		}
	}
	if s.answers == nil {
		return nil, errors.New("tierlist: answers storage is not configured")
	}
	t, err := s.readable(ctx, in.Slug)
	if err != nil {
		return nil, err
	}
	if t.Status != domain.StatusOpen {
		return nil, ErrNotAcceptingSubmissions
	}

	if !replace && s.lookup != nil {
		existing, err := s.lookup.SubmissionBySession(ctx, t.ID, in.SessionID)
		switch {
		case err == nil:
			return nil, &AlreadySubmittedError{ResultID: existing.ID}
		case !errors.Is(err, ErrNotFound):
			return nil, err
		}
	}

	id, err := s.newID()
	if err != nil {
		return nil, err
	}
	now := s.now().UTC()
	res, err := s.answers.Save(ctx, Submission{
		ID:         id,
		TierlistID: t.ID,
		SessionID:  in.SessionID,
		Placements: in.Placements,
	}, now)
	if err != nil {
		return nil, err
	}
	if !replace && res.Replaced {
		return nil, &AlreadySubmittedError{ResultID: res.SubmissionID}
	}

	rec := &SubmissionRecord{
		ID:         res.SubmissionID,
		TierlistID: t.ID,
		SessionID:  in.SessionID,
		Placements: append([]Placement(nil), in.Placements...),
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if s.lookup != nil {
		if stored, err := s.lookup.SubmissionByID(ctx, res.SubmissionID); err == nil {
			rec = stored
		}
	}
	return s.submissionDetails(ctx, rec, t, "")
}

// MySubmission returns the answer of a session or ErrNotFound.
func (s *Service) MySubmission(ctx context.Context, slug, sessionID string) (*SubmissionDetails, error) {
	if sessionID == "" {
		return nil, ErrNotFound
	}
	t, err := s.readable(ctx, slug)
	if err != nil {
		return nil, err
	}
	if s.lookup == nil {
		return nil, ErrNotFound
	}
	rec, err := s.lookup.SubmissionBySession(ctx, t.ID, sessionID)
	if err != nil {
		return nil, err
	}
	return s.submissionDetails(ctx, rec, t, "")
}

// Result returns a result by its public ID.
func (s *Service) Result(ctx context.Context, id, viewerID string) (*SubmissionDetails, error) {
	if s.lookup == nil {
		return nil, ErrNotFound
	}
	rec, err := s.lookup.SubmissionByID(ctx, id)
	if err != nil {
		return nil, err
	}
	t, err := s.store.GetByID(ctx, rec.TierlistID)
	if err != nil {
		return nil, err
	}
	if t.Status != domain.StatusOpen && t.Status != domain.StatusClosed {
		return nil, ErrNotFound
	}
	return s.submissionDetails(ctx, rec, t, viewerID)
}

func (s *Service) submissionDetails(ctx context.Context, rec *SubmissionRecord, t *domain.Tierlist, viewerID string) (*SubmissionDetails, error) {
	n, err := s.store.CountSubmissions(ctx, t.ID)
	if err != nil {
		return nil, err
	}
	d := &SubmissionDetails{
		Submission:       *rec,
		Tierlist:         t,
		IsOwner:          viewerID != "" && viewerID == t.AuthorID,
		SubmissionsCount: n,
	}
	if s.users != nil {
		prof, err := s.users.Profile(ctx, t.AuthorID)
		switch {
		case err == nil:
			d.AuthorName = prof.DisplayName
		case errors.Is(err, ErrNotFound):
		default:
			return nil, fmt.Errorf("tierlist: load author: %w", err)
		}
	}
	return d, nil
}

// TierCount is the number of answers that placed an item in one tier.
type TierCount struct {
	TierID  string
	Count   int
	Percent float64
}

// ItemStats is the answer distribution of one item across all tiers.
type ItemStats struct {
	ItemID       string
	Label        string
	Distribution []TierCount
}

// SimilarResult is another result and its similarity to the requested answer.
type SimilarResult struct {
	ResultID   string
	Similarity float64
}

// ResultStats is the anonymous statistics of one result.
type ResultStats struct {
	TotalSubmissions    int
	InsufficientData    bool
	MinSubmissions      int
	LikeYouPercent      *float64
	SimilarityThreshold float64
	Items               []ItemStats
	Similar             []SimilarResult
	Dissimilar          []SimilarResult
}

// Stats computes anonymous statistics for a result. No session or user
// identifiers are included.
func (s *Service) Stats(ctx context.Context, id string) (*ResultStats, error) {
	if s.answers == nil {
		return nil, errors.New("tierlist: answers storage is not configured")
	}
	res, err := s.Result(ctx, id, "")
	if err != nil {
		return nil, err
	}
	t := res.Tierlist
	all, err := s.answers.Answers(ctx, t.ID)
	if err != nil {
		return nil, err
	}
	dists, err := s.answers.Distributions(ctx, t.ID)
	if err != nil {
		return nil, err
	}

	itemIndex := make(map[string]domain.Item, len(t.Items))
	for _, item := range t.Items {
		itemIndex[item.ID] = item
	}

	tierByPosition := make([]domain.Tier, len(t.Tiers))
	for _, tier := range t.Tiers {
		if tier.Position >= 0 && tier.Position < len(tierByPosition) {
			tierByPosition[tier.Position] = tier
		}
	}

	var mine similarity.Answer
	others := make([]StoredAnswer, 0, len(all))
	for _, answer := range all {
		if answer.ID == id {
			mine = answer.Answer
			continue
		}
		others = append(others, answer)
	}
	if mine == nil {
		return nil, ErrNotFound
	}

	opts := similarity.DefaultOptions(len(t.Tiers))
	think := similarity.ThinkLikeYou(mine, storedAnswers(others), opts)

	stats := &ResultStats{
		TotalSubmissions:    len(all),
		InsufficientData:    think.NotEnoughData,
		MinSubmissions:      opts.MinAnswers,
		SimilarityThreshold: opts.Threshold,
		Items:               make([]ItemStats, 0, len(dists)),
		Similar:             make([]SimilarResult, 0, 5),
		Dissimilar:          make([]SimilarResult, 0, 5),
	}
	if !think.NotEnoughData {
		percent := think.Share * 100
		stats.LikeYouPercent = &percent
	}

	total := len(all)
	for _, dist := range dists {
		item, ok := itemIndex[dist.ItemID]
		if !ok {
			continue
		}
		out := ItemStats{
			ItemID:       dist.ItemID,
			Label:        item.Label,
			Distribution: make([]TierCount, 0, len(dist.Counts)),
		}
		for pos, count := range dist.Counts {
			if pos < 0 || pos >= len(tierByPosition) {
				continue
			}
			var percent float64
			if total > 0 {
				percent = math.Round(float64(count)*1000/float64(total)) / 10
			}
			out.Distribution = append(out.Distribution, TierCount{
				TierID: tierByPosition[pos].ID, Count: count, Percent: percent,
			})
		}
		stats.Items = append(stats.Items, out)
	}

	if think.NotEnoughData {
		return stats, nil
	}

	ranked := make([]SimilarResult, 0, len(others))
	for _, other := range others {
		score, ok := similarity.Similarity(mine, other.Answer, opts)
		if !ok {
			continue
		}
		ranked = append(ranked, SimilarResult{ResultID: other.ID, Similarity: score})
	}
	slices.SortFunc(ranked, func(a, b SimilarResult) int {
		switch {
		case a.Similarity > b.Similarity:
			return -1
		case a.Similarity < b.Similarity:
			return 1
		case a.ResultID < b.ResultID:
			return -1
		case a.ResultID > b.ResultID:
			return 1
		default:
			return 0
		}
	})
	for i := 0; i < len(ranked) && i < 5; i++ {
		stats.Similar = append(stats.Similar, ranked[i])
	}
	for i := len(ranked) - 1; i >= 0 && len(stats.Dissimilar) < 5; i-- {
		stats.Dissimilar = append(stats.Dissimilar, ranked[i])
	}
	return stats, nil
}

func storedAnswers(in []StoredAnswer) []similarity.Answer {
	out := make([]similarity.Answer, len(in))
	for i, answer := range in {
		out[i] = answer.Answer
	}
	return out
}
