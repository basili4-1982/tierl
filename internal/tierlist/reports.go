package tierlist

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"code.sarnas.ru/tierl/internal/domain"
)

// DefaultReportHideThreshold is the number of distinct reports after which a
// public tier list is hidden automatically.
const DefaultReportHideThreshold = 5

// DefaultHideReportsThreshold is an alias for DefaultReportHideThreshold.
const DefaultHideReportsThreshold = DefaultReportHideThreshold

// Report errors. Use errors.Is to test for them.
var (
	// ErrReportDuplicate is returned when the same reporter reports a tier list twice.
	ErrReportDuplicate = errors.New("tierlist: report already exists")
	// ErrReportNotFound is returned when a report or its tier list does not exist.
	ErrReportNotFound = errors.New("tierlist: report not found")
	// ErrTierlistHidden is returned when a report targets a hidden tier list.
	ErrTierlistHidden = errors.New("tierlist: tier list is hidden")
	// ErrNotModerator is returned when a caller without moderation rights tries
	// to change a moderation decision.
	ErrNotModerator = errors.New("tierlist: moderator required")
)

// Report is one complaint about a public tier list.
type Report struct {
	ID             string
	TierlistID     string
	ReporterKey    string
	ReporterIPHash string
	Reason         string
	Comment        string
	CreatedAt      time.Time
	ReportsCount   int
	Hidden         bool
}

// ReportInput is the input of Service.Report. ReporterKey is a stable,
// non-reversible identifier of the reporter supplied by the HTTP layer.
type ReportInput struct {
	Slug        string
	ReporterKey string
	// Aliases accepted by callers that name the identity after its source.
	ReporterID     string
	ReporterIPHash string
	SessionID      string
	Reason         string
	Comment        string
}

// ReportResult describes a stored report and the moderation state it caused.
type ReportResult struct {
	Report       Report
	ReportsCount int
	Hidden       bool
}

// ReportOutcome is the storage result of adding a report.
type ReportOutcome struct {
	ReportsCount int
	Hidden       bool
}

// ModerationAction is one append-only audit record.
type ModerationAction struct {
	ID           int64
	TierlistID   string
	ActorID      string
	ActorRole    string
	Action       string
	Reason       string
	ReportsCount int
	CreatedAt    time.Time
}

// ReportStore is the optional part of a Store used for moderation. Keeping it
// separate means existing Store implementations continue to satisfy Store.
type ReportStore interface {
	// AddReport inserts a report, updates the report counter and hides the
	// tier list when the number of reports reaches threshold. The hide and its
	// audit record happen in the same transaction.
	AddReport(ctx context.Context, r Report, threshold int, now time.Time) (ReportOutcome, error)
	// CountReports returns the number of reports for a tier list.
	CountReports(ctx context.Context, tierlistID string) (int, error)
	// AuditTrail returns moderation decisions for a tier list, newest first.
	AuditTrail(ctx context.Context, tierlistID string) ([]ModerationAction, error)
	// RecordModerationAction appends a manual moderation decision.
	RecordModerationAction(ctx context.Context, a ModerationAction) error
}

// SetReportHideThreshold overrides the automatic hide threshold. A value <= 0
// restores the default.
func (s *Service) SetReportHideThreshold(n int) {
	if n <= 0 {
		n = DefaultReportHideThreshold
	}
	s.reportThreshold = n
}

// Report hides a public tier list once the number of distinct reports reaches
// the configured threshold. Hidden tier lists are reported as not found, and
// reporting a non-public tier list does not reveal it.
func (s *Service) Report(ctx context.Context, in ReportInput) (*ReportResult, error) {
	reporter := firstNonEmpty(in.ReporterKey, in.ReporterIPHash, in.ReporterID, in.SessionID)
	if strings.TrimSpace(reporter) == "" {
		return nil, &domain.ValidationError{Field: "reporter", Reason: "must not be empty"}
	}
	reason := strings.TrimSpace(in.Reason)
	if reason == "" {
		reason = "other"
	}
	if len([]rune(reason)) > 100 {
		return nil, &domain.ValidationError{Field: "reason", Reason: "length exceeds 100"}
	}
	comment := strings.TrimSpace(in.Comment)
	if len([]rune(comment)) > 2000 {
		return nil, &domain.ValidationError{Field: "comment", Reason: "length exceeds 2000"}
	}

	t, err := s.readable(ctx, in.Slug)
	if err != nil {
		return nil, err
	}
	if t.Visibility != domain.VisibilityPublic {
		// Complaints are about the public catalog; an unlisted link is not
		// exposed as a moderation target.
		return nil, ErrNotFound
	}
	rs, ok := s.store.(ReportStore)
	if !ok {
		return nil, errors.New("tierlist: store does not support reports")
	}

	id, err := s.newID()
	if err != nil {
		return nil, err
	}
	now := s.now().UTC()
	r := Report{
		ID: id, TierlistID: t.ID, ReporterKey: reporter, ReporterIPHash: reporter,
		Reason: reason, Comment: comment, CreatedAt: now,
	}
	out, err := rs.AddReport(ctx, r, s.threshold(), now)
	if err != nil {
		return nil, err
	}
	r.ReportsCount, r.Hidden = out.ReportsCount, out.Hidden
	return &ReportResult{Report: r, ReportsCount: out.ReportsCount, Hidden: out.Hidden}, nil
}

// AddReport is an alias for Report.
func (s *Service) AddReport(ctx context.Context, in ReportInput) (*ReportResult, error) {
	return s.Report(ctx, in)
}

// ListAudit is an alias for AuditTrail.
func (s *Service) ListAudit(ctx context.Context, slug string) ([]ModerationAction, error) {
	return s.AuditTrail(ctx, slug)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// Hide applies a manual moderation decision and records it in the audit trail.
func (s *Service) Hide(ctx context.Context, actorID, actorRole, slug, reason string) (*Details, error) {
	if strings.TrimSpace(actorID) == "" {
		return nil, ErrNotModerator
	}
	t, err := s.store.GetBySlug(ctx, slug)
	if err != nil {
		return nil, err
	}
	if t.Status != domain.StatusHidden {
		now := s.now().UTC()
		if err := s.store.SetStatus(ctx, t.ID, domain.StatusHidden, now); err != nil {
			return nil, err
		}
		t.Status, t.UpdatedAt = domain.StatusHidden, now
		if err := s.recordAudit(ctx, t.ID, actorID, actorRole, "hide", reason, now); err != nil {
			return nil, err
		}
	}
	return s.details(ctx, t, actorID)
}

// Unhide reopens a hidden tier list and records the decision in the audit trail.
func (s *Service) Unhide(ctx context.Context, actorID, actorRole, slug, reason string) (*Details, error) {
	if strings.TrimSpace(actorID) == "" {
		return nil, ErrNotModerator
	}
	t, err := s.store.GetBySlug(ctx, slug)
	if err != nil {
		return nil, err
	}
	if t.Status == domain.StatusHidden {
		now := s.now().UTC()
		if err := s.store.SetStatus(ctx, t.ID, domain.StatusOpen, now); err != nil {
			return nil, err
		}
		t.Status, t.UpdatedAt = domain.StatusOpen, now
		if err := s.recordAudit(ctx, t.ID, actorID, actorRole, "unhide", reason, now); err != nil {
			return nil, err
		}
	}
	return s.details(ctx, t, actorID)
}

// ReportsCount returns the number of reports for a visible tier list.
func (s *Service) ReportsCount(ctx context.Context, slug string) (int, error) {
	t, err := s.readable(ctx, slug)
	if err != nil {
		return 0, err
	}
	rs, ok := s.store.(ReportStore)
	if !ok {
		return 0, errors.New("tierlist: store does not support reports")
	}
	return rs.CountReports(ctx, t.ID)
}

// AuditTrail returns moderation decisions about a tier list, including a
// hidden one (the caller is expected to be the author or a moderator).
func (s *Service) AuditTrail(ctx context.Context, slug string) ([]ModerationAction, error) {
	if err := domain.ValidateSlug(slug, domain.VisibilityPublic); err != nil {
		return nil, ErrNotFound
	}
	t, err := s.store.GetBySlug(ctx, slug)
	if err != nil {
		return nil, err
	}
	rs, ok := s.store.(ReportStore)
	if !ok {
		return nil, errors.New("tierlist: store does not support reports")
	}
	entries, err := rs.AuditTrail(ctx, t.ID)
	if err != nil {
		return nil, fmt.Errorf("tierlist: load audit: %w", err)
	}
	return entries, nil
}

func (s *Service) recordAudit(ctx context.Context, tierlistID, actorID, actorRole, action, reason string, now time.Time) error {
	rs, ok := s.store.(ReportStore)
	if !ok {
		return nil
	}
	if strings.TrimSpace(actorRole) == "" {
		actorRole = "moderator"
	}
	return rs.RecordModerationAction(ctx, ModerationAction{
		TierlistID: tierlistID, ActorID: actorID, ActorRole: actorRole,
		Action: action, Reason: reason, CreatedAt: now,
	})
}

func (s *Service) threshold() int {
	if s.reportThreshold <= 0 {
		return DefaultReportHideThreshold
	}
	return s.reportThreshold
}
