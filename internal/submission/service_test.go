package submission_test

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"testing"
	"time"

	"github.com/basili4-1982/tierl/internal/domain"
	"github.com/basili4-1982/tierl/internal/similarity"
	"github.com/basili4-1982/tierl/internal/submission"
	"github.com/basili4-1982/tierl/internal/tierlist"
)

var now = time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)

type fakeTierlists struct {
	bySlug map[string]*domain.Tierlist
	byID   map[string]*domain.Tierlist
}

func (f *fakeTierlists) GetBySlug(_ context.Context, slug string) (*domain.Tierlist, error) {
	t, ok := f.bySlug[slug]
	if !ok {
		return nil, tierlist.ErrNotFound
	}
	return t, nil
}

func (f *fakeTierlists) GetByID(_ context.Context, id string) (*domain.Tierlist, error) {
	t, ok := f.byID[id]
	if !ok {
		return nil, tierlist.ErrNotFound
	}
	return t, nil
}

func (f *fakeTierlists) CountSubmissions(context.Context, string) (int, error) { return 0, nil }

type fakeAnswers struct {
	subs    []tierlist.StoredSubmission
	tierPos map[string]int
}

func (f *fakeAnswers) CreateAnswer(_ context.Context, s tierlist.Submission, at time.Time) (tierlist.SaveResult, error) {
	if f.find(s.TierlistID, s.SessionID) != nil {
		return tierlist.SaveResult{}, tierlist.ErrAlreadySubmitted
	}
	f.add(s, at, at)
	return tierlist.SaveResult{SubmissionID: s.ID}, nil
}

func (f *fakeAnswers) Save(_ context.Context, s tierlist.Submission, at time.Time) (tierlist.SaveResult, error) {
	if old := f.find(s.TierlistID, s.SessionID); old != nil {
		id, created := old.ID, old.CreatedAt
		*old = tierlist.StoredSubmission{
			ID: id, TierlistID: s.TierlistID, TierlistSlug: "survey", SessionID: s.SessionID,
			Placements: s.Placements, CreatedAt: created, UpdatedAt: at,
		}
		return tierlist.SaveResult{SubmissionID: id, Replaced: true}, nil
	}
	f.add(s, at, at)
	return tierlist.SaveResult{SubmissionID: s.ID}, nil
}

func (f *fakeAnswers) add(s tierlist.Submission, created, updated time.Time) {
	f.subs = append(f.subs, tierlist.StoredSubmission{
		ID: s.ID, TierlistID: s.TierlistID, TierlistSlug: "survey", SessionID: s.SessionID,
		Placements: s.Placements, CreatedAt: created, UpdatedAt: updated,
	})
}

func (f *fakeAnswers) find(tierlistID, session string) *tierlist.StoredSubmission {
	for i := range f.subs {
		if f.subs[i].TierlistID == tierlistID && f.subs[i].SessionID == session {
			return &f.subs[i]
		}
	}
	return nil
}

func (f *fakeAnswers) Answers(_ context.Context, tierlistID string) ([]tierlist.StoredAnswer, error) {
	var out []tierlist.StoredAnswer
	for _, s := range f.subs {
		if s.TierlistID != tierlistID {
			continue
		}
		a := similarity.Answer{}
		for _, p := range s.Placements {
			a[similarity.ItemID(p.ItemID)] = f.tierPos[p.TierID]
		}
		out = append(out, tierlist.StoredAnswer{ID: s.ID, Answer: a})
	}
	return out, nil
}

func (f *fakeAnswers) Distributions(_ context.Context, tierlistID string) ([]tierlist.ItemDistribution, error) {
	counts := map[string][]int{}
	for _, s := range f.subs {
		if s.TierlistID != tierlistID {
			continue
		}
		for _, p := range s.Placements {
			if counts[p.ItemID] == nil {
				counts[p.ItemID] = make([]int, len(f.tierPos))
			}
			counts[p.ItemID][f.tierPos[p.TierID]]++
		}
	}
	var out []tierlist.ItemDistribution
	for item, c := range counts {
		out = append(out, tierlist.ItemDistribution{ItemID: item, Counts: c})
	}
	return out, nil
}

func (f *fakeAnswers) GetSubmissionByID(_ context.Context, id string) (*tierlist.StoredSubmission, error) {
	for i := range f.subs {
		if f.subs[i].ID == id {
			return &f.subs[i], nil
		}
	}
	return nil, tierlist.ErrNotFound
}

func (f *fakeAnswers) GetSubmissionBySession(_ context.Context, tierlistID, session string) (*tierlist.StoredSubmission, error) {
	for i := range f.subs {
		if f.subs[i].TierlistID == tierlistID && f.subs[i].SessionID == session {
			return &f.subs[i], nil
		}
	}
	return nil, tierlist.ErrNotFound
}

func (f *fakeAnswers) ListSubmissionsBySession(_ context.Context, session string) ([]tierlist.StoredSubmission, error) {
	var out []tierlist.StoredSubmission
	for _, s := range f.subs {
		if s.SessionID == session {
			out = append(out, s)
		}
	}
	return out, nil
}

type env struct {
	svc     *submission.Service
	tl      *domain.Tierlist
	answers *fakeAnswers
}

func newEnv(t *testing.T, status domain.Status) env {
	t.Helper()
	tl := &domain.Tierlist{
		ID: "tl", Slug: "survey", AuthorID: "anna", Title: "Survey",
		Visibility: domain.VisibilityPublic, Status: status, CreatedAt: now, UpdatedAt: now,
	}
	tierPos := map[string]int{}
	for i, name := range []string{"S", "A", "B"} {
		tl.Tiers = append(tl.Tiers, domain.Tier{ID: "t" + name, Name: name, Color: "#ff0000", Position: i})
		tierPos["t"+name] = i
	}
	for i := range 4 {
		tl.Items = append(tl.Items, domain.Item{ID: fmt.Sprintf("i%d", i), Label: fmt.Sprintf("Item %d", i), Position: i})
	}
	answers := &fakeAnswers{tierPos: tierPos}
	svc := submission.NewService(
		&fakeTierlists{bySlug: map[string]*domain.Tierlist{tl.Slug: tl}, byID: map[string]*domain.Tierlist{tl.ID: tl}},
		answers, nil,
		submission.Options{
			Threshold: 0.8, MinAnswers: 3, MinOverlap: 3,
			Now: func() time.Time { return now }, Rand: rand.New(rand.NewSource(1)), //nolint:gosec // deterministic test entropy
		},
	)
	return env{svc: svc, tl: tl, answers: answers}
}

func place(pairs ...string) []tierlist.Placement {
	var out []tierlist.Placement
	for i := 0; i < len(pairs); i += 2 {
		out = append(out, tierlist.Placement{ItemID: pairs[i], TierID: pairs[i+1]})
	}
	return out
}

func TestCreateValidatesPlacements(t *testing.T) {
	e := newEnv(t, domain.StatusOpen)
	ctx := context.Background()

	if _, err := e.svc.Create(ctx, submission.SubmitInput{Slug: "survey", SessionID: "s",
		Placements: place("i0", "tS", "i1", "tA")}); !errors.Is(err, submission.ErrTooFewPlacements) {
		t.Fatalf("two placements: %v", err)
	}
	for name, p := range map[string][]tierlist.Placement{
		"unknown item":   place("ghost", "tS", "i1", "tA", "i2", "tB"),
		"unknown tier":   place("i0", "ghost", "i1", "tA", "i2", "tB"),
		"duplicate item": place("i0", "tS", "i0", "tA", "i2", "tB"),
	} {
		if _, err := e.svc.Create(ctx, submission.SubmitInput{Slug: "survey", SessionID: "s", Placements: p}); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}

	sub, err := e.svc.Create(ctx, submission.SubmitInput{Slug: "survey", SessionID: "s",
		Placements: place("i0", "tS", "i1", "tA", "i2", "tB")})
	if err != nil {
		t.Fatal(err)
	}
	if sub.TierlistSlug != "survey" || len(sub.Placements) != 3 || sub.ID == "" {
		t.Fatalf("unexpected submission: %+v", sub)
	}
}

func TestCreateRejectsNonOpen(t *testing.T) {
	ctx := context.Background()
	for status, want := range map[domain.Status]error{
		domain.StatusClosed: tierlist.ErrNotAcceptingSubmissions,
		domain.StatusDraft:  tierlist.ErrNotFound,
		domain.StatusHidden: tierlist.ErrNotFound,
	} {
		e := newEnv(t, status)
		_, err := e.svc.Create(ctx, submission.SubmitInput{Slug: "survey", SessionID: "s",
			Placements: place("i0", "tS", "i1", "tA", "i2", "tB")})
		if !errors.Is(err, want) {
			t.Errorf("%s: %v, want %v", status, err, want)
		}
	}
	if _, err := newEnv(t, domain.StatusOpen).svc.Create(ctx, submission.SubmitInput{Slug: "missing", SessionID: "s",
		Placements: place("i0", "tS", "i1", "tA", "i2", "tB")}); !errors.Is(err, tierlist.ErrNotFound) {
		t.Errorf("missing: %v", err)
	}
}

func TestCreateDuplicateAndReplace(t *testing.T) {
	e := newEnv(t, domain.StatusOpen)
	ctx := context.Background()
	first, err := e.svc.Create(ctx, submission.SubmitInput{Slug: "survey", SessionID: "s",
		Placements: place("i0", "tS", "i1", "tA", "i2", "tB")})
	if err != nil {
		t.Fatal(err)
	}

	_, err = e.svc.Create(ctx, submission.SubmitInput{Slug: "survey", SessionID: "s",
		Placements: place("i0", "tB", "i1", "tB", "i2", "tB")})
	var already *submission.AlreadySubmittedError
	if !errors.As(err, &already) || already.ResultID != first.ID {
		t.Fatalf("duplicate: %v", err)
	}

	replaced, err := e.svc.Replace(ctx, submission.SubmitInput{Slug: "survey", SessionID: "s",
		Placements: place("i0", "tB", "i1", "tB", "i2", "tB")})
	if err != nil {
		t.Fatal(err)
	}
	if replaced.ID != first.ID || len(e.answers.subs) != 1 {
		t.Fatalf("replace: %+v, %d answers", replaced, len(e.answers.subs))
	}
	if e.answers.subs[0].Placements[0].TierID != "tB" {
		t.Fatalf("placements not replaced: %+v", e.answers.subs[0].Placements)
	}

	// PUT without a previous answer creates one.
	other, err := e.svc.Replace(ctx, submission.SubmitInput{Slug: "survey", SessionID: "new",
		Placements: place("i0", "tS", "i1", "tS", "i2", "tS")})
	if err != nil || other.ID == first.ID || len(e.answers.subs) != 2 {
		t.Fatalf("upsert: %v %+v", err, other)
	}
}

func TestMineAndListMine(t *testing.T) {
	e := newEnv(t, domain.StatusOpen)
	ctx := context.Background()
	sub, err := e.svc.Create(ctx, submission.SubmitInput{Slug: "survey", SessionID: "s",
		Placements: place("i0", "tS", "i1", "tA", "i2", "tB")})
	if err != nil {
		t.Fatal(err)
	}
	got, err := e.svc.Mine(ctx, "survey", "s")
	if err != nil || got.ID != sub.ID {
		t.Fatalf("mine: %v %+v", err, got)
	}
	if _, err := e.svc.Mine(ctx, "survey", "other"); !errors.Is(err, tierlist.ErrNotFound) {
		t.Fatalf("missing mine: %v", err)
	}
	list, err := e.svc.ListMine(ctx, "s")
	if err != nil || len(list) != 1 || list[0].ID != sub.ID {
		t.Fatalf("list: %v %+v", err, list)
	}
}

// addAnswer stores an answer whose tier indexes are given per item i0..i3.
func (e env) addAnswer(t *testing.T, id, session string, tiers ...string) tierlist.StoredSubmission {
	t.Helper()
	ctx := context.Background()
	pairs := make([]string, 0, len(tiers)*2)
	for i, tier := range tiers {
		pairs = append(pairs, fmt.Sprintf("i%d", i), tier)
	}
	if _, err := e.svc.Create(ctx, submission.SubmitInput{Slug: "survey", SessionID: session, Placements: place(pairs...)}); err != nil {
		t.Fatalf("add answer %s: %v", session, err)
	}
	got, err := e.answers.GetSubmissionBySession(ctx, "tl", session)
	if err != nil {
		t.Fatal(err)
	}
	got.ID = id
	return *got
}

func TestStats(t *testing.T) {
	e := newEnv(t, domain.StatusOpen)
	// Target: everything in S. Total 4 answers, one identical, one one-tier
	// off, one opposite.
	target := e.addAnswer(t, "target", "target", "tS", "tS", "tS")
	_ = e.addAnswer(t, "same", "same", "tS", "tS", "tS")
	_ = e.addAnswer(t, "close", "close", "tA", "tA", "tA")
	_ = e.addAnswer(t, "far", "far", "tB", "tB", "tB")
	e.answers.subs[0].Placements = target.Placements

	stats, err := e.svc.Stats(context.Background(), "target")
	if err != nil {
		t.Fatal(err)
	}
	if stats.InsufficientData || stats.TotalSubmissions != 4 || stats.MinSubmissions != 3 {
		t.Fatalf("meta: %+v", stats)
	}
	if stats.LikeYouPercent == nil || math.Abs(*stats.LikeYouPercent-33.3) > 1e-9 {
		t.Fatalf("percent: %v", stats.LikeYouPercent)
	}
	if stats.SimilarityThreshold != 0.8 {
		t.Errorf("threshold: %v", stats.SimilarityThreshold)
	}
	if len(stats.Similar) != 3 || stats.Similar[0].Similarity != 1 || stats.Similar[1].Similarity != 0.5 {
		t.Errorf("similar: %+v", stats.Similar)
	}
	if len(stats.Dissimilar) != 3 || stats.Dissimilar[0].Similarity != 0 {
		t.Errorf("dissimilar: %+v", stats.Dissimilar)
	}
	if len(stats.Items) != 4 || len(stats.Items[0].Counts) != 3 {
		t.Fatalf("items: %+v", stats.Items)
	}
	// i0: two S (target, same), one A, one B => 50 / 25 / 25.
	want := []float64{50, 25, 25}
	for i, c := range stats.Items[0].Counts {
		if c.Count != []int{2, 1, 1}[i] || c.Percent != want[i] {
			t.Errorf("i0 tier %d: %+v", i, c)
		}
	}
}

func TestStatsInsufficientData(t *testing.T) {
	e := newEnv(t, domain.StatusOpen)
	e.addAnswer(t, "target", "target", "tS", "tS", "tS")
	e.addAnswer(t, "other", "other", "tA", "tA", "tA")
	stats, err := e.svc.Stats(context.Background(), "target")
	if err != nil {
		t.Fatal(err)
	}
	if !stats.InsufficientData || stats.LikeYouPercent != nil || len(stats.Similar) != 0 || len(stats.Dissimilar) != 0 {
		t.Fatalf("insufficient: %+v", stats)
	}
	if stats.TotalSubmissions != 2 {
		t.Errorf("total: %d", stats.TotalSubmissions)
	}
}

func TestStatsTopN(t *testing.T) {
	e := newEnv(t, domain.StatusOpen)
	e.addAnswer(t, "target", "target", "tS", "tS", "tS")
	for i := range 8 {
		tier := []string{"tS", "tA", "tB"}[i%3]
		e.addAnswer(t, fmt.Sprintf("s%d", i), fmt.Sprintf("s%d", i), tier, tier, tier)
	}
	stats, err := e.svc.Stats(context.Background(), "target")
	if err != nil {
		t.Fatal(err)
	}
	if len(stats.Similar) != submission.TopN || len(stats.Dissimilar) != submission.TopN {
		t.Fatalf("top-N: %d / %d", len(stats.Similar), len(stats.Dissimilar))
	}
}

func TestResultVisibility(t *testing.T) {
	ctx := context.Background()
	for status, want := range map[domain.Status]error{
		domain.StatusOpen:   nil,
		domain.StatusClosed: nil,
		domain.StatusHidden: tierlist.ErrNotFound,
		domain.StatusDraft:  tierlist.ErrNotFound,
	} {
		e := newEnv(t, status)
		sub, err := e.svc.Create(ctx, submission.SubmitInput{Slug: "survey", SessionID: "s",
			Placements: place("i0", "tS", "i1", "tA", "i2", "tB")})
		if err != nil {
			// Non-open surveys reject answers; seed the answer directly.
			e.answers.add(tierlist.Submission{ID: "sub", TierlistID: "tl", SessionID: "s",
				Placements: place("i0", "tS", "i1", "tA", "i2", "tB")}, now, now)
		} else {
			e.answers.subs[len(e.answers.subs)-1].ID = sub.ID
		}
		_, err = e.svc.Result(ctx, e.answers.subs[0].ID, "")
		if !errors.Is(err, want) {
			t.Errorf("%s: %v, want %v", status, err, want)
		}
	}
}
