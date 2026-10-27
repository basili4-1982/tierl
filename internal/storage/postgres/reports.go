package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/basili4-1982/tierl/internal/domain"
	"github.com/basili4-1982/tierl/internal/tierlist"
)

const reportReporterConstraint = "reports_unique_reporter"

var _ tierlist.ReportStore = (*Store)(nil)

// AddReport implements tierlist.ReportStore. The tier list row is locked so
// concurrent reports cannot both observe a count below the threshold, and the
// automatic hide plus its audit record commit together with the report.
func (s *Store) AddReport(ctx context.Context, r tierlist.Report, threshold int, now time.Time) (tierlist.ReportOutcome, error) {
	if threshold <= 0 {
		threshold = tierlist.DefaultReportHideThreshold
	}
	var out tierlist.ReportOutcome
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		var status string
		err := tx.QueryRow(ctx, `SELECT status FROM tierlists WHERE id = $1 FOR UPDATE`, r.TierlistID).Scan(&status)
		if errors.Is(err, pgx.ErrNoRows) {
			return tierlist.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("postgres: lock reported tierlist: %w", err)
		}
		if domain.Status(status) == domain.StatusHidden {
			return tierlist.ErrTierlistHidden
		}

		if _, err := tx.Exec(ctx, `
			INSERT INTO reports (id, tierlist_id, reporter_ip_hash, reason, comment, created_at)
			VALUES ($1, $2, $3, $4, $5, $6)`,
			r.ID, r.TierlistID, r.ReporterKey, r.Reason, r.Comment, r.CreatedAt); err != nil {
			if isReportCollision(err) {
				return tierlist.ErrReportDuplicate
			}
			return mapWriteErr("create report", err)
		}

		var count int
		if err := tx.QueryRow(ctx, `
			SELECT count(*) FROM reports WHERE tierlist_id = $1`, r.TierlistID).Scan(&count); err != nil {
			return fmt.Errorf("postgres: count reports: %w", err)
		}
		out.ReportsCount = count

		hide := count >= threshold
		hiddenReason := "auto:threshold"
		if _, err := tx.Exec(ctx, `
			UPDATE tierlists
			SET reports_count = $2,
			    status        = CASE WHEN $3 THEN 'hidden' ELSE status END,
			    hidden_at     = CASE WHEN $3 THEN COALESCE(hidden_at, $4) ELSE hidden_at END,
			    hidden_reason = CASE WHEN $3 THEN COALESCE(hidden_reason, $5) ELSE hidden_reason END,
			    updated_at    = CASE WHEN $3 THEN GREATEST($4, created_at) ELSE updated_at END
			WHERE id = $1`, r.TierlistID, count, hide, now, hiddenReason); err != nil {
			return mapWriteErr("update report count", err)
		}

		if hide {
			out.Hidden = true
			if _, err := tx.Exec(ctx, `
				INSERT INTO moderation_audit
					(tierlist_id, actor_id, actor_role, action, reason, reports_count, created_at)
				VALUES ($1, NULL, 'system', 'auto_hide', $2, $3, $4)`,
				r.TierlistID, hiddenReason, count, now); err != nil {
				return mapWriteErr("record auto hide", err)
			}
		}
		return nil
	})
	if err != nil {
		return tierlist.ReportOutcome{}, err
	}
	return out, nil
}

// CountReports implements tierlist.ReportStore.
func (s *Store) CountReports(ctx context.Context, tierlistID string) (int, error) {
	var n int
	if err := s.pool.QueryRow(ctx, `
		SELECT count(*) FROM reports WHERE tierlist_id = $1`, tierlistID).Scan(&n); err != nil {
		return 0, fmt.Errorf("postgres: count reports: %w", err)
	}
	return n, nil
}

// AuditTrail implements tierlist.ReportStore.
func (s *Store) AuditTrail(ctx context.Context, tierlistID string) ([]tierlist.ModerationAction, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, tierlist_id, COALESCE(actor_id, ''), actor_role, action,
		       reason, reports_count, created_at
		FROM moderation_audit
		WHERE tierlist_id = $1
		ORDER BY created_at DESC, id DESC`, tierlistID)
	if err != nil {
		return nil, fmt.Errorf("postgres: list audit: %w", err)
	}
	defer rows.Close()

	out := make([]tierlist.ModerationAction, 0)
	for rows.Next() {
		var a tierlist.ModerationAction
		if err := rows.Scan(&a.ID, &a.TierlistID, &a.ActorID, &a.ActorRole, &a.Action,
			&a.Reason, &a.ReportsCount, &a.CreatedAt); err != nil {
			return nil, fmt.Errorf("postgres: scan audit: %w", err)
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: list audit: %w", err)
	}
	return out, nil
}

// RecordModerationAction implements tierlist.ReportStore.
func (s *Store) RecordModerationAction(ctx context.Context, a tierlist.ModerationAction) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO moderation_audit
			(tierlist_id, actor_id, actor_role, action, reason, reports_count, created_at)
		VALUES ($1, NULLIF($2, ''), $3, $4, $5, $6, $7)`,
		a.TierlistID, a.ActorID, a.ActorRole, a.Action, a.Reason, a.ReportsCount, a.CreatedAt)
	if err != nil {
		return mapWriteErr("record moderation action", err)
	}
	return nil
}

// isReportCollision reports whether err violates the one-report-per-reporter
// constraint.
func isReportCollision(err error) bool {
	pe := pgErr(err)
	return pe != nil && pe.Code == codeUniqueViolation &&
		(pe.ConstraintName == reportReporterConstraint || pe.ConstraintName == "")
}
