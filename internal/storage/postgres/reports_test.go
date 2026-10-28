package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"code.sarnas.ru/tierl/internal/domain"
	"code.sarnas.ru/tierl/internal/tierlist"
)

func TestReportThresholdAndAutoHide(t *testing.T) {
	s, pool := newStore(t)
	ctx := context.Background()
	if err := s.Create(ctx, mk("rp", "reports-list", domain.VisibilityPublic, domain.StatusOpen, t0)); err != nil {
		t.Fatal(err)
	}

	now := t0.Add(time.Hour)
	report := func(id, reporter, reason string) tierlist.Report {
		return tierlist.Report{
			ID: id, TierlistID: "rp", ReporterKey: reporter, ReporterIPHash: reporter,
			Reason: reason, CreatedAt: now,
		}
	}
	state := func() (status string, count int, hidden bool, hiddenReason string) {
		t.Helper()
		if err := pool.QueryRow(ctx, `
			SELECT status, reports_count, hidden_at IS NOT NULL, COALESCE(hidden_reason, '')
			FROM tierlists WHERE id = $1`, "rp").Scan(&status, &count, &hidden, &hiddenReason); err != nil {
			t.Fatal(err)
		}
		return status, count, hidden, hiddenReason
	}

	out, err := s.AddReport(ctx, report("r1", "user:bob", "spam"), 2, now)
	if err != nil {
		t.Fatalf("first report: %v", err)
	}
	if out.ReportsCount != 1 || out.Hidden {
		t.Fatalf("first outcome = %+v", out)
	}
	status, count, hidden, reason := state()
	if status != string(domain.StatusOpen) || count != 1 || hidden || reason != "" {
		t.Fatalf("state after first report = %s %d %v %q", status, count, hidden, reason)
	}

	if _, err := s.AddReport(ctx, report("r2", "user:bob", "spam"), 2, now); !errors.Is(err, tierlist.ErrReportDuplicate) {
		t.Fatalf("duplicate: want ErrReportDuplicate, got %v", err)
	}
	if _, count, _, _ := state(); count != 1 {
		t.Fatalf("duplicate changed report count: %d", count)
	}

	out, err = s.AddReport(ctx, report("r3", "user:carol", "abuse"), 2, now)
	if err != nil {
		t.Fatalf("second report: %v", err)
	}
	if out.ReportsCount != 2 || !out.Hidden {
		t.Fatalf("second outcome = %+v", out)
	}
	status, count, hidden, reason = state()
	if status != string(domain.StatusHidden) || count != 2 || !hidden || reason != "auto:threshold" {
		t.Fatalf("state after threshold = %s %d %v %q", status, count, hidden, reason)
	}
	if n, err := s.CountReports(ctx, "rp"); err != nil || n != 2 {
		t.Fatalf("CountReports = %d, %v", n, err)
	}

	audit, err := s.AuditTrail(ctx, "rp")
	if err != nil {
		t.Fatal(err)
	}
	if len(audit) != 1 {
		t.Fatalf("audit = %+v", audit)
	}
	a := audit[0]
	if a.Action != "auto_hide" || a.ActorRole != "system" || a.ActorID != "" ||
		a.Reason != "auto:threshold" || a.ReportsCount != 2 || !a.CreatedAt.Equal(now) {
		t.Fatalf("audit = %+v", a)
	}

	if _, err := s.AddReport(ctx, report("r4", "user:dave", "spam"), 2, now); !errors.Is(err, tierlist.ErrTierlistHidden) {
		t.Fatalf("report on hidden: want ErrTierlistHidden, got %v", err)
	}
	if n, _ := s.CountReports(ctx, "rp"); n != 2 {
		t.Fatalf("report on hidden changed count: %d", n)
	}
}
