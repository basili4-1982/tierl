// Package submission implements the participant-facing flow: accepting
// anonymous answers, returning the participant's own answer, and computing
// the result and aggregate statistics from ADR-001.
package submission

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"time"

	"github.com/basili4-1982/tierl/internal/domain"
	"github.com/basili4-1982/tierl/internal/similarity"
	"github.com/basili4-1982/tierl/internal/tierlist"
)

// MinPlacements is the minimal answer completeness (SPEC section 4).
const MinPlacements = 3

// TopN is how many most and least similar answers the statistics expose.
const TopN = 5

// Errors specific to accepting answers.
var (
	// ErrTooFewPlacements is returned for an answer with fewer than
	// MinPlacements placed items.
	ErrTooFewPlacements = errors.New("submission: too few placements")
	// ErrAlreadySubmitted is returned when POST is repeated for a session;
	// use Replace to change the answer.
	ErrAlreadySubmitted = errors.New("submission: already submitted")
)

// AlreadySubmittedError carries the ID of the existing result so the client
// can navigate to it.
type AlreadySubmittedError struct {
	ResultID string
}

// Error implements error.
func (e *AlreadySubmittedError) Error() string {
	return fmt.Sprintf("submission: already submitted (result %s)", e.ResultID)
}

// Unwrap lets errors.Is(err, ErrAlreadySubmitted) succeed.
func (e *AlreadySubmittedError) Unwrap() error { return ErrAlreadySubmitted }

// Tierlists reads tier lists for the submission flow.
type Tierlists interface {
	GetBySlug(ctx context.Context, slug string) (*domain.Tierlist, error)
	GetByID(ctx context.Context, id string) (*domain.Tierlist, error)
	CountSubmissions(ctx context.Context, id string) (int, error)
}

// Users resolves the display name of the author.
type Users interface {
	Profile(ctx context.Context, userID string) (tierlist.Profile, error)
}

// Options configure a Service. Zero values select the defaults from ADR-001.
type Options struct {
	// Threshold is the minimal similarity counted as "thinks like you".
	Threshold float64
	// MinAnswers is the number of answers below which only "not enough data"
	// is shown.
	MinAnswers int
	// MinOverlap is the minimal number of common items for a comparable pair.
	MinOverlap int
	// Now returns the current time; defaults to time.Now.
	Now func() time.Time
	// Rand is the entropy source for submission IDs; defaults to crypto/rand.
	Rand io.Reader
}

// Service accepts answers and builds results and statistics.
type Service struct {
	tierlists  Tierlists
	answers    tierlist.Answers
	users      Users
	threshold  float64
	minAnswer  int
	minOverlap int
	now        func() time.Time
	rand       io.Reader
}

// NewService builds a Service.
func NewService(tierlists Tierlists, answers tierlist.Answers, users Users, opts Options) *Service {
	if opts.Threshold <= 0 {
		opts.Threshold = similarity.DefaultThreshold
	}
	if opts.MinAnswers <= 0 {
		opts.MinAnswers = similarity.DefaultMinAnswers
	}
	if opts.MinOverlap <= 0 {
		opts.MinOverlap = similarity.DefaultMinOverlap
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Rand == nil {
		opts.Rand = rand.Reader
	}
	return &Service{
		tierlists: tierlists, answers: answers, users: users,
		threshold: opts.Threshold, minAnswer: opts.MinAnswers, minOverlap: opts.MinOverlap,
		now: opts.Now, rand: opts.Rand,
	}
}

// SubmitInput is an answer sent by a participant.
type SubmitInput struct {
	Slug       string
	SessionID  string
	Placements []tierlist.Placement
}

// Submission is a stored answer without any personal identifiers.
type Submission struct {
	ID           string
	TierlistID   string
	TierlistSlug string
	Placements   []tierlist.Placement
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Result is one answer together with its tier list.
type Result struct {
	Submission       Submission
	Tierlist         *domain.Tierlist
	AuthorName       string
	IsOwner          bool
	SubmissionsCount int
}

// TierCount is the number of answers that placed an item into one tier.
type TierCount struct {
	TierID  string
	Count   int
	Percent float64
}

// ItemDistribution is the per-tier distribution of one item.
type ItemDistribution struct {
	ItemID string
	Label  string
	Counts []TierCount
}

// Similar is one comparable answer, reduced to its similarity. It has no
// identifier: a result link belongs to its participant only.
type Similar struct {
	Similarity float64
}

// Stats is the aggregate result view from SPEC section 5.
type Stats struct {
	TotalSubmissions    int
	InsufficientData    bool
	MinSubmissions      int
	LikeYouPercent      *float64
	SimilarityThreshold float64
	Items               []ItemDistribution
	Similar             []Similar
	Dissimilar          []Similar
}

// Create stores the first answer of a session. It returns
// *AlreadySubmittedError when the session already answered.
func (s *Service) Create(ctx context.Context, in SubmitInput) (*Submission, error) {
	tl, err := s.openTierlist(ctx, in.Slug)
	if err != nil {
		return nil, err
	}
	placements, err := validatePlacements(tl, in.Placements)
	if err != nil {
		return nil, err
	}
	id, err := s.newID()
	if err != nil {
		return nil, err
	}
	res, err := s.answers.CreateAnswer(ctx, tierlist.Submission{
		ID: id, TierlistID: tl.ID, SessionID: in.SessionID, Placements: placements,
	}, s.now().UTC())
	if errors.Is(err, tierlist.ErrAlreadySubmitted) {
		existing, getErr := s.answers.GetSubmissionBySession(ctx, tl.ID, in.SessionID)
		if getErr != nil {
			// Keep the conflict even if the follow-up read failed: the client
			// must not retry a POST that was already stored.
			return nil, &AlreadySubmittedError{}
		}
		return nil, &AlreadySubmittedError{ResultID: existing.ID}
	}
	if err != nil {
		return nil, err
	}
	return s.load(ctx, res.SubmissionID)
}

// Replace stores the answer of a session, replacing an earlier one and keeping
// its result ID. A session without an answer gets a new one (upsert).
func (s *Service) Replace(ctx context.Context, in SubmitInput) (*Submission, error) {
	tl, err := s.openTierlist(ctx, in.Slug)
	if err != nil {
		return nil, err
	}
	placements, err := validatePlacements(tl, in.Placements)
	if err != nil {
		return nil, err
	}

	id := ""
	if existing, err := s.answers.GetSubmissionBySession(ctx, tl.ID, in.SessionID); err == nil {
		id = existing.ID
	} else if !errors.Is(err, tierlist.ErrNotFound) {
		return nil, err
	}
	if id == "" {
		if id, err = s.newID(); err != nil {
			return nil, err
		}
	}

	res, err := s.answers.Save(ctx, tierlist.Submission{
		ID: id, TierlistID: tl.ID, SessionID: in.SessionID, Placements: placements,
	}, s.now().UTC())
	if err != nil {
		return nil, err
	}
	return s.load(ctx, res.SubmissionID)
}

// Mine returns the answer of a session in a tier list, or ErrNotFound.
func (s *Service) Mine(ctx context.Context, slug, sessionID string) (*Submission, error) {
	tl, err := s.readable(ctx, slug)
	if err != nil {
		return nil, err
	}
	stored, err := s.answers.GetSubmissionBySession(ctx, tl.ID, sessionID)
	if err != nil {
		return nil, err
	}
	return fromStored(stored), nil
}

// ListMine returns every answer of a session, newest change first.
func (s *Service) ListMine(ctx context.Context, sessionID string) ([]Submission, error) {
	stored, err := s.answers.ListSubmissionsBySession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	out := make([]Submission, 0, len(stored))
	for i := range stored {
		out = append(out, *fromStored(&stored[i]))
	}
	return out, nil
}

// Result returns one answer and its tier list. Hidden and draft tier lists are
// reported as not found; closed ones stay readable. viewerID is empty for a
// guest and only feeds is_owner.
func (s *Service) Result(ctx context.Context, id, viewerID string) (*Result, error) {
	stored, err := s.answers.GetSubmissionByID(ctx, id)
	if err != nil {
		return nil, err
	}
	tl, err := s.readableByID(ctx, stored.TierlistID)
	if err != nil {
		return nil, err
	}
	n, err := s.tierlists.CountSubmissions(ctx, tl.ID)
	if err != nil {
		return nil, err
	}
	r := &Result{
		Submission: *fromStored(stored), Tierlist: tl,
		IsOwner:          viewerID != "" && viewerID == tl.AuthorID,
		SubmissionsCount: n,
	}
	if s.users != nil {
		if prof, err := s.users.Profile(ctx, tl.AuthorID); err == nil {
			r.AuthorName = prof.DisplayName
		} else if !errors.Is(err, tierlist.ErrNotFound) {
			return nil, fmt.Errorf("submission: load author: %w", err)
		}
	}
	return r, nil
}

// Stats computes "thinks like you", the per-item tier distribution and the
// most and least similar answers (without identifiers).
func (s *Service) Stats(ctx context.Context, id string) (*Stats, error) {
	stored, err := s.answers.GetSubmissionByID(ctx, id)
	if err != nil {
		return nil, err
	}
	tl, err := s.readableByID(ctx, stored.TierlistID)
	if err != nil {
		return nil, err
	}
	all, err := s.answers.Answers(ctx, tl.ID)
	if err != nil {
		return nil, err
	}
	dist, err := s.answers.Distributions(ctx, tl.ID)
	if err != nil {
		return nil, err
	}

	itemIDs := itemOrder(tl)
	target := answerPositions(tl, stored.Placements)
	others := make([]similarity.Answer, 0, len(all))
	for _, a := range all {
		if a.ID != stored.ID {
			others = append(others, a.Answer)
		}
	}

	opts := similarity.Options{
		Tiers: len(tl.Tiers), MinOverlap: s.minOverlap,
		Threshold: s.threshold, MinAnswers: s.minAnswer,
	}
	think := similarity.ThinkLikeYou(target, others, opts)

	out := &Stats{
		TotalSubmissions: think.Answers, InsufficientData: think.NotEnoughData,
		MinSubmissions: s.minAnswer, SimilarityThreshold: s.threshold,
		Items: distributions(tl, itemIDs, dist), Similar: []Similar{}, Dissimilar: []Similar{},
	}
	if !think.NotEnoughData {
		pct := math.Round(think.Share*1000) / 10
		out.LikeYouPercent = &pct
		out.Similar, out.Dissimilar = rank(target, others, opts)
	}
	return out, nil
}

// openTierlist loads a tier list that may accept answers. Draft and hidden
// lists are not found; closed lists return ErrNotAcceptingSubmissions.
func (s *Service) openTierlist(ctx context.Context, slug string) (*domain.Tierlist, error) {
	tl, err := s.readable(ctx, slug)
	if err != nil {
		return nil, err
	}
	if tl.Status != domain.StatusOpen {
		return nil, tierlist.ErrNotAcceptingSubmissions
	}
	return tl, nil
}

// readable loads a visible tier list by slug.
func (s *Service) readable(ctx context.Context, slug string) (*domain.Tierlist, error) {
	if err := domain.ValidateSlug(slug, domain.VisibilityPublic); err != nil {
		return nil, tierlist.ErrNotFound
	}
	tl, err := s.tierlists.GetBySlug(ctx, slug)
	if err != nil {
		return nil, err
	}
	if tl.Status != domain.StatusOpen && tl.Status != domain.StatusClosed {
		return nil, tierlist.ErrNotFound
	}
	return tl, nil
}

// readableByID loads a visible tier list by ID.
func (s *Service) readableByID(ctx context.Context, id string) (*domain.Tierlist, error) {
	tl, err := s.tierlists.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if tl.Status != domain.StatusOpen && tl.Status != domain.StatusClosed {
		return nil, tierlist.ErrNotFound
	}
	return tl, nil
}

func (s *Service) load(ctx context.Context, id string) (*Submission, error) {
	stored, err := s.answers.GetSubmissionByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return fromStored(stored), nil
}

func fromStored(s *tierlist.StoredSubmission) *Submission {
	return &Submission{
		ID: s.ID, TierlistID: s.TierlistID, TierlistSlug: s.TierlistSlug,
		Placements: append([]tierlist.Placement(nil), s.Placements...),
		CreatedAt:  s.CreatedAt.UTC(), UpdatedAt: s.UpdatedAt.UTC(),
	}
}

// validatePlacements checks completeness and that every item and tier belongs
// to the tier list.
func validatePlacements(tl *domain.Tierlist, in []tierlist.Placement) ([]tierlist.Placement, error) {
	if len(in) < MinPlacements {
		return nil, ErrTooFewPlacements
	}
	if len(in) > len(tl.Items) {
		return nil, &domain.ValidationError{Field: "placements", Reason: fmt.Sprintf("at most %d items can be placed", len(tl.Items))}
	}
	items := make(map[string]struct{}, len(tl.Items))
	for _, it := range tl.Items {
		items[it.ID] = struct{}{}
	}
	tiers := make(map[string]struct{}, len(tl.Tiers))
	for _, tr := range tl.Tiers {
		tiers[tr.ID] = struct{}{}
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]tierlist.Placement, 0, len(in))
	for _, p := range in {
		if _, dup := seen[p.ItemID]; dup {
			return nil, &domain.ValidationError{Field: "placements", Reason: "an item is placed twice"}
		}
		seen[p.ItemID] = struct{}{}
		if _, ok := items[p.ItemID]; !ok {
			return nil, &domain.ValidationError{Field: "placements", Reason: "unknown item"}
		}
		if _, ok := tiers[p.TierID]; !ok {
			return nil, &domain.ValidationError{Field: "placements", Reason: "unknown tier"}
		}
		out = append(out, p)
	}
	return out, nil
}

// answerPositions turns placements into tier positions for similarity.
func answerPositions(tl *domain.Tierlist, placements []tierlist.Placement) similarity.Answer {
	pos := make(map[string]int, len(tl.Tiers))
	for _, tr := range tl.Tiers {
		pos[tr.ID] = tr.Position
	}
	a := make(similarity.Answer, len(placements))
	for _, p := range placements {
		a[similarity.ItemID(p.ItemID)] = pos[p.TierID]
	}
	return a
}

// itemOrder returns item IDs sorted by their position.
func itemOrder(tl *domain.Tierlist) []string {
	items := append([]domain.Item(nil), tl.Items...)
	sort.Slice(items, func(i, j int) bool { return items[i].Position < items[j].Position })
	ids := make([]string, len(items))
	for i, it := range items {
		ids[i] = it.ID
	}
	return ids
}

// distributions converts the pre-aggregate into the contract shape. Percent
// is the share of answers that placed the item (skipped items do not dilute
// it), one decimal place.
func distributions(tl *domain.Tierlist, itemIDs []string, dist []tierlist.ItemDistribution) []ItemDistribution {
	tiers := append([]domain.Tier(nil), tl.Tiers...)
	sort.Slice(tiers, func(i, j int) bool { return tiers[i].Position < tiers[j].Position })
	labels := make(map[string]string, len(tl.Items))
	for _, it := range tl.Items {
		labels[it.ID] = it.Label
	}
	byItem := make(map[string][]int, len(dist))
	for _, d := range dist {
		byItem[d.ItemID] = d.Counts
	}
	out := make([]ItemDistribution, 0, len(itemIDs))
	for _, id := range itemIDs {
		counts := byItem[id]
		total := 0
		for _, n := range counts {
			total += n
		}
		row := ItemDistribution{ItemID: id, Label: labels[id], Counts: make([]TierCount, len(tiers))}
		for i, tr := range tiers {
			n := 0
			if tr.Position >= 0 && tr.Position < len(counts) {
				n = counts[tr.Position]
			}
			pct := 0.0
			if total > 0 {
				pct = math.Round(float64(n)/float64(total)*1000) / 10
			}
			row.Counts[i] = TierCount{TierID: tr.ID, Count: n, Percent: pct}
		}
		out = append(out, row)
	}
	return out
}

// rank returns the top-N most and least similar answers by similarity.
func rank(target similarity.Answer, others []similarity.Answer, opts similarity.Options) ([]Similar, []Similar) {
	type scored struct {
		sim float64
		idx int
	}
	var all []scored
	for i, q := range others {
		if sim, ok := similarity.Similarity(target, q, opts); ok {
			all = append(all, scored{sim: sim, idx: i})
		}
	}
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].sim == all[j].sim {
			return all[i].idx < all[j].idx
		}
		return all[i].sim > all[j].sim
	})
	similar := make([]Similar, 0, min(TopN, len(all)))
	for i := 0; i < len(all) && i < TopN; i++ {
		similar = append(similar, Similar{Similarity: round4(all[i].sim)})
	}
	dissimilar := make([]Similar, 0, min(TopN, len(all)))
	start := max(0, len(all)-TopN)
	for i := start; i < len(all); i++ {
		dissimilar = append(dissimilar, Similar{Similarity: round4(all[i].sim)})
	}
	// Least similar first.
	sort.Slice(dissimilar, func(i, j int) bool { return dissimilar[i].Similarity < dissimilar[j].Similarity })
	return similar, dissimilar
}

func round4(v float64) float64 { return math.Round(v*10000) / 10000 }

// newID returns a random UUID v4.
func (s *Service) newID() (string, error) {
	var b [16]byte
	if _, err := io.ReadFull(s.rand, b[:]); err != nil {
		return "", fmt.Errorf("%w: %w", domain.ErrRandom, err)
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}
