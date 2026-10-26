package tierlist_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/basili4-1982/tierl/internal/domain"
	"github.com/basili4-1982/tierl/internal/tierlist"
)

func placements(tl *tierlist.Details, tier int) []tierlist.Placement {
	out := make([]tierlist.Placement, 0, len(tl.Tierlist.Items))
	for _, item := range tl.Tierlist.Items {
		out = append(out, tierlist.Placement{ItemID: item.ID, TierID: tl.Tierlist.Tiers[tier].ID})
	}
	return out
}

func createList(t *testing.T) (*tierlist.Service, *tierlist.Details) {
	t.Helper()
	e := newEnv(t)
	d, err := e.svc.Create(context.Background(), "anna", validCreate())
	if err != nil {
		t.Fatal(err)
	}
	return e.svc, d
}

func TestSubmitRequiresThreePlacements(t *testing.T) {
	svc, list := createList(t)
	_, err := svc.Submit(context.Background(), tierlist.SubmitInput{
		Slug: list.Tierlist.Slug, SessionID: "s1", Placements: placements(list, 0)[:2],
	}, false)
	if !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("err = %v", err)
	}
}

func TestSubmitOnePerSessionAndReplace(t *testing.T) {
	ctx := context.Background()
	svc, list := createList(t)
	first, err := svc.Submit(ctx, tierlist.SubmitInput{
		Slug: list.Tierlist.Slug, SessionID: "same", Placements: placements(list, 0),
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	var already *tierlist.AlreadySubmittedError
	if _, err := svc.Submit(ctx, tierlist.SubmitInput{
		Slug: list.Tierlist.Slug, SessionID: "same", Placements: placements(list, 1),
	}, false); !errors.As(err, &already) || already.ResultID != first.Submission.ID {
		t.Fatalf("second answer err = %v, want already_submitted %q", err, first.Submission.ID)
	}

	replaced, err := svc.Submit(ctx, tierlist.SubmitInput{
		Slug: list.Tierlist.Slug, SessionID: "same", Placements: placements(list, 1),
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	if replaced.Submission.ID != first.Submission.ID {
		t.Fatalf("replace changed result id: %q -> %q", first.Submission.ID, replaced.Submission.ID)
	}
	if replaced.Submission.Placements[0].TierID != list.Tierlist.Tiers[1].ID {
		t.Fatalf("placements not replaced: %+v", replaced.Submission.Placements)
	}
	mine, err := svc.MySubmission(ctx, list.Tierlist.Slug, "same")
	if err != nil || mine.Submission.ID != first.Submission.ID {
		t.Fatalf("mine = %+v, %v", mine, err)
	}
	if _, err := svc.MySubmission(ctx, list.Tierlist.Slug, "other"); !errors.Is(err, tierlist.ErrNotFound) {
		t.Fatalf("other session: %v", err)
	}
}

func TestResultAndStatsBoundaries(t *testing.T) {
	ctx := context.Background()
	svc, list := createList(t)
	slug := list.Tierlist.Slug
	var firstID string
	for i := 0; i < 19; i++ {
		d, err := svc.Submit(ctx, tierlist.SubmitInput{
			Slug: slug, SessionID: fmt.Sprintf("s%02d", i), Placements: placements(list, 0),
		}, false)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			firstID = d.Submission.ID
		}
	}

	res, err := svc.Result(ctx, firstID, "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Submission.ID != firstID || res.Tierlist.Slug != slug || len(res.Submission.Placements) != 3 {
		t.Fatalf("result = %+v", res)
	}

	stats, err := svc.Stats(ctx, firstID)
	if err != nil {
		t.Fatal(err)
	}
	if !stats.InsufficientData || stats.MinSubmissions != 20 || stats.LikeYouPercent != nil {
		t.Fatalf("19 answers stats = %+v", stats)
	}
	if len(stats.Similar) != 0 || len(stats.Dissimilar) != 0 {
		t.Fatalf("similar/dissimilar must be empty: %+v %+v", stats.Similar, stats.Dissimilar)
	}
	if len(stats.Items) != 3 {
		t.Fatalf("items = %d", len(stats.Items))
	}
	for _, item := range stats.Items {
		if len(item.Distribution) != 2 || item.Distribution[0].Count != 19 || item.Distribution[0].Percent != 100 {
			t.Fatalf("distribution = %+v", item)
		}
	}

	// Exactly 20 answers is enough data.
	last, err := svc.Submit(ctx, tierlist.SubmitInput{
		Slug: slug, SessionID: "s19", Placements: placements(list, 0),
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	stats, err = svc.Stats(ctx, firstID)
	if err != nil {
		t.Fatal(err)
	}
	if stats.InsufficientData || stats.TotalSubmissions != 20 || stats.LikeYouPercent == nil || *stats.LikeYouPercent != 100 {
		t.Fatalf("20 answers stats = %+v", stats)
	}
	if len(stats.Similar) != 5 || len(stats.Dissimilar) != 5 {
		t.Fatalf("similar=%d dissimilar=%d", len(stats.Similar), len(stats.Dissimilar))
	}
	for _, s := range stats.Similar {
		if s.ResultID == firstID || s.Similarity != 1 {
			t.Fatalf("similar = %+v", s)
		}
	}
	if _, err := svc.Result(ctx, "missing", ""); !errors.Is(err, tierlist.ErrNotFound) {
		t.Fatalf("missing result: %v", err)
	}
	_ = last
}
