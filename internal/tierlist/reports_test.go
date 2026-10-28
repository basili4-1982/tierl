package tierlist_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"code.sarnas.ru/tierl/internal/domain"
	"code.sarnas.ru/tierl/internal/tierlist"
	"code.sarnas.ru/tierl/internal/tierlist/tierlisttest"
)

func newReportEnv(t *testing.T, threshold int) env {
	t.Helper()
	store := tierlisttest.NewMemStore()
	users := tierlisttest.Users{
		"anna": {DisplayName: "Anna", EmailVerified: true},
		"bob":  {DisplayName: "Bob", EmailVerified: true},
	}
	svc := tierlist.NewService(store, users, tierlist.ServiceDeps{
		Now:                 func() time.Time { return fixedNow },
		ReportHideThreshold: threshold,
	})
	return env{svc: svc, store: store}
}

func TestReportThresholdAndAudit(t *testing.T) {
	ctx := context.Background()
	e := newReportEnv(t, 2)
	d, err := e.svc.Create(ctx, "anna", validCreate())
	if err != nil {
		t.Fatal(err)
	}
	slug := d.Tierlist.Slug

	first, err := e.svc.Report(ctx, tierlist.ReportInput{Slug: slug, ReporterKey: "user:bob", Reason: "spam"})
	if err != nil {
		t.Fatalf("first report: %v", err)
	}
	if first.ReportsCount != 1 || first.Hidden {
		t.Fatalf("first report = %+v", first)
	}
	tl, err := e.store.GetBySlug(ctx, slug)
	if err != nil {
		t.Fatal(err)
	}
	if tl.Status != domain.StatusOpen {
		t.Fatalf("status after first report = %s", tl.Status)
	}

	if _, err := e.svc.Report(ctx, tierlist.ReportInput{Slug: slug, ReporterKey: "user:bob", Reason: "spam"}); !errors.Is(err, tierlist.ErrReportDuplicate) {
		t.Fatalf("duplicate report: want ErrReportDuplicate, got %v", err)
	}

	second, err := e.svc.Report(ctx, tierlist.ReportInput{Slug: slug, ReporterKey: "user:carol", Reason: "abuse"})
	if err != nil {
		t.Fatalf("second report: %v", err)
	}
	if second.ReportsCount != 2 || !second.Hidden {
		t.Fatalf("second report = %+v", second)
	}
	tl, err = e.store.GetBySlug(ctx, slug)
	if err != nil {
		t.Fatal(err)
	}
	if tl.Status != domain.StatusHidden {
		t.Fatalf("status at threshold = %s, want hidden", tl.Status)
	}

	audit, err := e.svc.AuditTrail(ctx, slug)
	if err != nil {
		t.Fatal(err)
	}
	if len(audit) != 1 {
		t.Fatalf("audit = %+v", audit)
	}
	a := audit[0]
	if a.Action != "auto_hide" || a.ActorRole != "system" || a.ActorID != "" ||
		a.Reason != "auto:threshold" || a.ReportsCount != 2 || !a.CreatedAt.Equal(fixedNow) {
		t.Fatalf("audit = %+v", a)
	}

	if _, err := e.svc.Report(ctx, tierlist.ReportInput{Slug: slug, ReporterKey: "user:dave"}); !errors.Is(err, tierlist.ErrNotFound) {
		t.Fatalf("report on hidden: want ErrNotFound, got %v", err)
	}
	if n, err := e.svc.ReportsCount(ctx, slug); !errors.Is(err, tierlist.ErrNotFound) {
		t.Fatalf("ReportsCount on hidden: %v (n=%d)", err, n)
	}
}

func TestReportRequiresPublicTierlist(t *testing.T) {
	ctx := context.Background()
	e := newReportEnv(t, 5)

	in := validCreate()
	in.Visibility = domain.VisibilityUnlisted
	in.Slug = "ignored"
	d, err := e.svc.Create(ctx, "anna", in)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Report(ctx, tierlist.ReportInput{Slug: d.Tierlist.Slug, ReporterKey: "user:bob"}); !errors.Is(err, tierlist.ErrNotFound) {
		t.Fatalf("unlisted report: want ErrNotFound, got %v", err)
	}
	if _, err := e.svc.Report(ctx, tierlist.ReportInput{Slug: "missing", ReporterKey: "user:bob"}); !errors.Is(err, tierlist.ErrNotFound) {
		t.Fatalf("missing report: want ErrNotFound, got %v", err)
	}

	d, err = e.svc.Create(ctx, "anna", validCreate())
	if err != nil {
		t.Fatal(err)
	}
	res, err := e.svc.Report(ctx, tierlist.ReportInput{Slug: d.Tierlist.Slug, ReporterKey: "user:bob"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Report.Reason != "other" {
		t.Fatalf("default reason = %q, want other", res.Report.Reason)
	}
}
