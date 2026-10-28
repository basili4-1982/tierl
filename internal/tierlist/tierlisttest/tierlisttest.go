// Package tierlisttest provides in-memory fakes of the tierlist dependencies
// for unit tests of the service and the HTTP handlers.
package tierlisttest

import (
	"context"
	"encoding/base64"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"code.sarnas.ru/tierl/internal/domain"
	"code.sarnas.ru/tierl/internal/similarity"
	"code.sarnas.ru/tierl/internal/tierlist"
)

// MemStore is an in-memory tierlist.Store.
type MemStore struct {
	mu          sync.Mutex
	byID        map[string]domain.Tierlist
	subs        map[string]int
	submissions map[string]*memSubmission
	reports     map[string]map[string]tierlist.Report
	audit       []tierlist.ModerationAction
	auditID     int64

	// Errs makes the named method (for example "Create") fail with the error.
	Errs map[string]error
}

type memSubmission struct {
	tierlist.SubmissionRecord
}

var _ tierlist.Store = (*MemStore)(nil)

// NewMemStore returns an empty store.
func NewMemStore() *MemStore {
	return &MemStore{
		byID:        map[string]domain.Tierlist{},
		subs:        map[string]int{},
		submissions: map[string]*memSubmission{},
		reports:     map[string]map[string]tierlist.Report{},
		Errs:        map[string]error{},
	}
}

// SetSubmissions sets the number of answers of a tier list.
func (m *MemStore) SetSubmissions(id string, n int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.subs[id] = n
}

// Put stores a copy of t directly, bypassing the checks.
func (m *MemStore) Put(t domain.Tierlist) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.byID[t.ID] = clone(t)
}

// Len returns the number of stored tier lists.
func (m *MemStore) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.byID)
}

func clone(t domain.Tierlist) domain.Tierlist {
	t.Items = slices.Clone(t.Items)
	t.Tiers = slices.Clone(t.Tiers)
	return t
}

func (m *MemStore) bySlug(slug string) (domain.Tierlist, bool) {
	for _, t := range m.byID {
		if t.Slug == slug {
			return t, true
		}
	}
	return domain.Tierlist{}, false
}

// Create implements tierlist.Store.
func (m *MemStore) Create(_ context.Context, t *domain.Tierlist) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.Errs["Create"]; err != nil {
		return err
	}
	if _, taken := m.bySlug(t.Slug); taken {
		return tierlist.ErrSlugTaken
	}
	m.byID[t.ID] = clone(*t)
	return nil
}

// GetBySlug implements tierlist.Store.
func (m *MemStore) GetBySlug(_ context.Context, slug string) (*domain.Tierlist, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.Errs["GetBySlug"]; err != nil {
		return nil, err
	}
	t, ok := m.bySlug(slug)
	if !ok {
		return nil, tierlist.ErrNotFound
	}
	c := clone(t)
	return &c, nil
}

// GetByID implements tierlist.Store.
func (m *MemStore) GetByID(_ context.Context, id string) (*domain.Tierlist, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.Errs["GetByID"]; err != nil {
		return nil, err
	}
	t, ok := m.byID[id]
	if !ok {
		return nil, tierlist.ErrNotFound
	}
	c := clone(t)
	return &c, nil
}

// Update implements tierlist.Store.
func (m *MemStore) Update(_ context.Context, t *domain.Tierlist) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.Errs["Update"]; err != nil {
		return err
	}
	cur, ok := m.byID[t.ID]
	if !ok {
		return tierlist.ErrNotFound
	}
	if other, taken := m.bySlug(t.Slug); taken && other.ID != t.ID {
		return tierlist.ErrSlugTaken
	}
	if (m.subs[t.ID] > 0 || m.countByTierlist(t.ID) > 0) && (!slices.Equal(cur.Items, t.Items) || !slices.Equal(cur.Tiers, t.Tiers)) {
		return domain.ErrStructureLocked
	}
	m.byID[t.ID] = clone(*t)
	return nil
}

// SetStatus implements tierlist.Store.
func (m *MemStore) SetStatus(_ context.Context, id string, status domain.Status, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.Errs["SetStatus"]; err != nil {
		return err
	}
	t, ok := m.byID[id]
	if !ok {
		return tierlist.ErrNotFound
	}
	t.Status, t.UpdatedAt = status, now
	t.ClosedAt = nil
	if status == domain.StatusClosed {
		closedAt := now
		t.ClosedAt = &closedAt
	}
	m.byID[id] = t
	return nil
}

// Delete implements tierlist.Store.
func (m *MemStore) Delete(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.Errs["Delete"]; err != nil {
		return err
	}
	if _, ok := m.byID[id]; !ok {
		return tierlist.ErrNotFound
	}
	delete(m.byID, id)
	delete(m.subs, id)
	delete(m.reports, id)
	m.audit = slices.DeleteFunc(m.audit, func(a tierlist.ModerationAction) bool { return a.TierlistID == id })
	for sid, rec := range m.submissions {
		if rec.TierlistID == id {
			delete(m.submissions, sid)
		}
	}
	return nil
}

// CountSubmissions implements tierlist.Store.
func (m *MemStore) CountSubmissions(_ context.Context, id string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.Errs["CountSubmissions"]; err != nil {
		return 0, err
	}
	if _, ok := m.byID[id]; !ok {
		return 0, tierlist.ErrNotFound
	}
	if n := m.countByTierlist(id); n > 0 {
		return n, nil
	}
	return m.subs[id], nil
}

func (m *MemStore) countByTierlist(id string) int {
	n := 0
	for _, rec := range m.submissions {
		if rec.TierlistID == id {
			n++
		}
	}
	return n
}

// ListPublic implements tierlist.Store and mirrors the real catalog rules:
// public+open only, optional search, sort and keyset pagination.
func (m *MemStore) ListPublic(_ context.Context, p tierlist.PageParams) (tierlist.CatalogPage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.Errs["ListPublic"]; err != nil {
		return tierlist.CatalogPage{}, err
	}
	sortBy := strings.ToLower(strings.TrimSpace(p.Sort))
	switch sortBy {
	case "":
		sortBy = tierlist.CatalogSortNew
	case tierlist.CatalogSortNew, tierlist.CatalogSortPopular:
	default:
		return tierlist.CatalogPage{}, tierlist.ErrBadSort
	}
	limit := p.Limit
	switch {
	case limit <= 0:
		limit = tierlist.DefaultPageSize
	case limit > tierlist.MaxPageSize:
		limit = tierlist.MaxPageSize
	}

	var cur memCursor
	hasCursor := false
	if p.Cursor != "" {
		decoded, err := decodeMemCursor(p.Cursor, sortBy)
		if err != nil {
			return tierlist.CatalogPage{}, err
		}
		cur, hasCursor = decoded, true
	}

	query := strings.ToLower(p.Query)
	entries := make([]tierlist.CatalogEntry, 0)
	for _, t := range m.byID {
		if t.Visibility != domain.VisibilityPublic || t.Status != domain.StatusOpen {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(t.Title), query) &&
			!strings.Contains(strings.ToLower(t.Description), query) {
			continue
		}
		n := m.countByTierlist(t.ID)
		if n == 0 {
			n = m.subs[t.ID]
		}
		if hasCursor && !memCursorBefore(cur, memCursorFor(t, n), sortBy) {
			continue
		}
		entries = append(entries, tierlist.CatalogEntry{
			ID: t.ID, AuthorID: t.AuthorID, Slug: t.Slug, Title: t.Title,
			Description: t.Description, ItemCount: len(t.Items), Submissions: n,
			CreatedAt: t.CreatedAt,
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		return memEntryBefore(entries[i], entries[j], sortBy)
	})
	page := tierlist.CatalogPage{Entries: entries}
	if len(page.Entries) > limit {
		page.Entries = page.Entries[:limit]
		last := page.Entries[limit-1]
		page.NextCursor = encodeMemCursor(last, sortBy)
	}
	return page, nil
}

type memCursor struct {
	createdAt   time.Time
	id          string
	submissions int
	empty       bool
}

func memCursorFor(t domain.Tierlist, submissions int) memCursor {
	return memCursor{createdAt: t.CreatedAt, id: t.ID, submissions: submissions}
}

func memEntryBefore(a, b tierlist.CatalogEntry, sortBy string) bool {
	if sortBy == tierlist.CatalogSortPopular && a.Submissions != b.Submissions {
		return a.Submissions > b.Submissions
	}
	if !a.CreatedAt.Equal(b.CreatedAt) {
		return a.CreatedAt.After(b.CreatedAt)
	}
	return a.ID > b.ID
}

// memCursorBefore reports whether e comes strictly after cur in the sort order.
func memCursorBefore(cur, e memCursor, sortBy string) bool {
	if cur.empty {
		return true
	}
	if sortBy == tierlist.CatalogSortPopular && e.submissions != cur.submissions {
		return e.submissions < cur.submissions
	}
	if !e.createdAt.Equal(cur.createdAt) {
		return e.createdAt.Before(cur.createdAt)
	}
	return e.id < cur.id
}

func encodeMemCursor(e tierlist.CatalogEntry, sortBy string) string {
	raw := strconv.FormatInt(e.CreatedAt.UnixMicro(), 10) + "|" + e.ID
	if sortBy == tierlist.CatalogSortPopular {
		raw += "|" + strconv.Itoa(e.Submissions)
	}
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeMemCursor(c, sortBy string) (memCursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(c)
	if err != nil {
		return memCursor{}, tierlist.ErrBadCursor
	}
	parts := strings.Split(string(raw), "|")
	want := 2
	if sortBy == tierlist.CatalogSortPopular {
		want = 3
	}
	if len(parts) != want || parts[1] == "" {
		return memCursor{}, tierlist.ErrBadCursor
	}
	micro, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return memCursor{}, tierlist.ErrBadCursor
	}
	cur := memCursor{createdAt: time.UnixMicro(micro).UTC(), id: parts[1]}
	if want == 3 {
		n, err := strconv.Atoi(parts[2])
		if err != nil || n < 0 {
			return memCursor{}, tierlist.ErrBadCursor
		}
		cur.submissions = n
	}
	return cur, nil
}

// ListByOwner implements tierlist.OwnerStore.
func (m *MemStore) ListByOwner(_ context.Context, userID string) ([]tierlist.OwnerEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.Errs["ListByOwner"]; err != nil {
		return nil, err
	}
	if err := m.Errs["ListPublic"]; err != nil {
		return nil, err
	}
	out := make([]tierlist.OwnerEntry, 0)
	for _, t := range m.byID {
		if t.AuthorID != userID {
			continue
		}
		n := m.countByTierlist(t.ID)
		if n == 0 {
			n = m.subs[t.ID]
		}
		out = append(out, tierlist.OwnerEntry{
			ID: t.ID, Slug: t.Slug, AuthorID: t.AuthorID, Title: t.Title,
			Description: t.Description, Visibility: t.Visibility, Status: t.Status,
			ItemCount: len(t.Items), Submissions: n, CreatedAt: t.CreatedAt,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID > out[j].ID
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out, nil
}

// SetStatusForOwner implements tierlist.OwnerStore.
func (m *MemStore) SetStatusForOwner(_ context.Context, id, userID string, status domain.Status, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.Errs["SetStatusForOwner"]; err != nil {
		return err
	}
	if err := m.Errs["SetStatus"]; err != nil {
		return err
	}
	t, ok := m.byID[id]
	if !ok || t.AuthorID != userID {
		return tierlist.ErrNotFound
	}
	t.Status, t.UpdatedAt = status, now
	t.ClosedAt = nil
	if status == domain.StatusClosed {
		closedAt := now
		t.ClosedAt = &closedAt
	}
	m.byID[id] = t
	return nil
}

// DeleteForOwner implements tierlist.OwnerStore.
func (m *MemStore) DeleteForOwner(_ context.Context, id, userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.Errs["DeleteForOwner"]; err != nil {
		return err
	}
	if err := m.Errs["Delete"]; err != nil {
		return err
	}
	t, ok := m.byID[id]
	if !ok || t.AuthorID != userID {
		return tierlist.ErrNotFound
	}
	delete(m.byID, id)
	delete(m.subs, id)
	delete(m.reports, id)
	m.audit = slices.DeleteFunc(m.audit, func(a tierlist.ModerationAction) bool { return a.TierlistID == id })
	for sid, rec := range m.submissions {
		if rec.TierlistID == id {
			delete(m.submissions, sid)
		}
	}
	return nil
}

// AddReport implements tierlist.ReportStore.
func (m *MemStore) AddReport(_ context.Context, r tierlist.Report, threshold int, now time.Time) (tierlist.ReportOutcome, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.Errs["AddReport"]; err != nil {
		return tierlist.ReportOutcome{}, err
	}
	t, ok := m.byID[r.TierlistID]
	if !ok {
		return tierlist.ReportOutcome{}, tierlist.ErrNotFound
	}
	if t.Status == domain.StatusHidden {
		return tierlist.ReportOutcome{}, tierlist.ErrTierlistHidden
	}
	if threshold <= 0 {
		threshold = tierlist.DefaultReportHideThreshold
	}
	byReporter := m.reports[r.TierlistID]
	if byReporter == nil {
		byReporter = map[string]tierlist.Report{}
		m.reports[r.TierlistID] = byReporter
	}
	if _, dup := byReporter[r.ReporterKey]; dup {
		return tierlist.ReportOutcome{}, tierlist.ErrReportDuplicate
	}
	r.ReportsCount = len(byReporter) + 1
	byReporter[r.ReporterKey] = r
	out := tierlist.ReportOutcome{ReportsCount: r.ReportsCount}
	if r.ReportsCount >= threshold {
		out.Hidden = true
		t.Status, t.UpdatedAt, t.ClosedAt = domain.StatusHidden, now, nil
		m.byID[t.ID] = t
		m.auditID++
		m.audit = append(m.audit, tierlist.ModerationAction{
			ID: m.auditID, TierlistID: t.ID, ActorRole: "system",
			Action: "auto_hide", Reason: "auto:threshold",
			ReportsCount: r.ReportsCount, CreatedAt: now,
		})
	}
	return out, nil
}

// CountReports implements tierlist.ReportStore.
func (m *MemStore) CountReports(_ context.Context, tierlistID string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.Errs["CountReports"]; err != nil {
		return 0, err
	}
	return len(m.reports[tierlistID]), nil
}

// AuditTrail implements tierlist.ReportStore.
func (m *MemStore) AuditTrail(_ context.Context, tierlistID string) ([]tierlist.ModerationAction, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.Errs["AuditTrail"]; err != nil {
		return nil, err
	}
	out := make([]tierlist.ModerationAction, 0)
	for i := len(m.audit) - 1; i >= 0; i-- {
		if m.audit[i].TierlistID == tierlistID {
			out = append(out, m.audit[i])
		}
	}
	return out, nil
}

// RecordModerationAction implements tierlist.ReportStore.
func (m *MemStore) RecordModerationAction(_ context.Context, a tierlist.ModerationAction) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.Errs["RecordModerationAction"]; err != nil {
		return err
	}
	m.auditID++
	a.ID = m.auditID
	m.audit = append(m.audit, a)
	return nil
}

// ReportsLen returns the number of stored reports of a tier list.
func (m *MemStore) ReportsLen(tierlistID string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.reports[tierlistID])
}

// Users is a map-backed tierlist.Users.
type Users map[string]tierlist.Profile

var _ tierlist.Users = Users(nil)

// Profile implements tierlist.Users.
func (u Users) Profile(_ context.Context, id string) (tierlist.Profile, error) {
	p, ok := u[id]
	if !ok {
		return tierlist.Profile{}, tierlist.ErrNotFound
	}
	return p, nil
}

// Save implements tierlist.Answers.
func (m *MemStore) Save(_ context.Context, sub tierlist.Submission, now time.Time) (tierlist.SaveResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.Errs["Save"]; err != nil {
		return tierlist.SaveResult{}, err
	}
	t, ok := m.byID[sub.TierlistID]
	if !ok {
		return tierlist.SaveResult{}, tierlist.ErrNotFound
	}
	if t.Status != domain.StatusOpen {
		return tierlist.SaveResult{}, tierlist.ErrNotAcceptingSubmissions
	}
	items := make(map[string]struct{}, len(t.Items))
	for _, it := range t.Items {
		items[it.ID] = struct{}{}
	}
	tiers := make(map[string]struct{}, len(t.Tiers))
	for _, tr := range t.Tiers {
		tiers[tr.ID] = struct{}{}
	}
	seen := make(map[string]struct{}, len(sub.Placements))
	for _, p := range sub.Placements {
		if _, dup := seen[p.ItemID]; dup {
			return tierlist.SaveResult{}, domain.ErrInvalid
		}
		seen[p.ItemID] = struct{}{}
		if _, ok := items[p.ItemID]; !ok {
			return tierlist.SaveResult{}, domain.ErrInvalid
		}
		if _, ok := tiers[p.TierID]; !ok {
			return tierlist.SaveResult{}, domain.ErrInvalid
		}
	}

	var existing *memSubmission
	for _, rec := range m.submissions {
		if rec.TierlistID == sub.TierlistID && rec.SessionID == sub.SessionID {
			existing = rec
			break
		}
	}
	now = now.UTC()
	if existing != nil {
		existing.Placements = append([]tierlist.Placement(nil), sub.Placements...)
		if now.After(existing.UpdatedAt) {
			existing.UpdatedAt = now
		}
		return tierlist.SaveResult{SubmissionID: existing.ID, Replaced: true}, nil
	}
	if _, exists := m.submissions[sub.ID]; exists {
		return tierlist.SaveResult{}, domain.ErrInvalid
	}
	m.submissions[sub.ID] = &memSubmission{SubmissionRecord: tierlist.SubmissionRecord{
		ID: sub.ID, TierlistID: sub.TierlistID, SessionID: sub.SessionID,
		Placements: append([]tierlist.Placement(nil), sub.Placements...),
		CreatedAt:  now, UpdatedAt: now,
	}}
	return tierlist.SaveResult{SubmissionID: sub.ID}, nil
}

// Answers implements tierlist.Answers.
func (m *MemStore) Answers(_ context.Context, tierlistID string) ([]tierlist.StoredAnswer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.Errs["Answers"]; err != nil {
		return nil, err
	}
	t, ok := m.byID[tierlistID]
	if !ok {
		return nil, tierlist.ErrNotFound
	}
	tierPos := make(map[string]int, len(t.Tiers))
	for _, tr := range t.Tiers {
		tierPos[tr.ID] = tr.Position
	}
	recs := make([]tierlist.SubmissionRecord, 0)
	for _, rec := range m.submissions {
		if rec.TierlistID == tierlistID {
			recs = append(recs, rec.SubmissionRecord)
		}
	}
	sort.Slice(recs, func(i, j int) bool {
		if recs[i].CreatedAt.Equal(recs[j].CreatedAt) {
			return recs[i].ID < recs[j].ID
		}
		return recs[i].CreatedAt.Before(recs[j].CreatedAt)
	})
	out := make([]tierlist.StoredAnswer, 0, len(recs))
	for _, rec := range recs {
		answer := similarity.Answer{}
		for _, p := range rec.Placements {
			answer[similarity.ItemID(p.ItemID)] = tierPos[p.TierID]
		}
		out = append(out, tierlist.StoredAnswer{ID: rec.ID, Answer: answer})
	}
	return out, nil
}

// Distributions implements tierlist.Answers.
func (m *MemStore) Distributions(_ context.Context, tierlistID string) ([]tierlist.ItemDistribution, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.Errs["Distributions"]; err != nil {
		return nil, err
	}
	t, ok := m.byID[tierlistID]
	if !ok {
		return nil, tierlist.ErrNotFound
	}
	tierPos := make(map[string]int, len(t.Tiers))
	for _, tr := range t.Tiers {
		tierPos[tr.ID] = tr.Position
	}
	items := append([]domain.Item(nil), t.Items...)
	sort.Slice(items, func(i, j int) bool { return items[i].Position < items[j].Position })
	out := make([]tierlist.ItemDistribution, 0, len(items))
	for _, item := range items {
		counts := make([]int, len(t.Tiers))
		for _, rec := range m.submissions {
			if rec.TierlistID != tierlistID {
				continue
			}
			for _, p := range rec.Placements {
				if p.ItemID == item.ID {
					if pos, ok := tierPos[p.TierID]; ok && pos >= 0 && pos < len(counts) {
						counts[pos]++
					}
				}
			}
		}
		out = append(out, tierlist.ItemDistribution{ItemID: item.ID, Counts: counts})
	}
	return out, nil
}

// SubmissionByID implements tierlist.SubmissionLookup.
func (m *MemStore) SubmissionByID(_ context.Context, id string) (*tierlist.SubmissionRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.Errs["SubmissionByID"]; err != nil {
		return nil, err
	}
	rec, ok := m.submissions[id]
	if !ok {
		return nil, tierlist.ErrNotFound
	}
	c := rec.SubmissionRecord
	c.Placements = append([]tierlist.Placement(nil), rec.Placements...)
	return &c, nil
}

// SubmissionBySession implements tierlist.SubmissionLookup.
func (m *MemStore) SubmissionBySession(_ context.Context, tierlistID, sessionID string) (*tierlist.SubmissionRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.Errs["SubmissionBySession"]; err != nil {
		return nil, err
	}
	for _, rec := range m.submissions {
		if rec.TierlistID == tierlistID && rec.SessionID == sessionID {
			c := rec.SubmissionRecord
			c.Placements = append([]tierlist.Placement(nil), rec.Placements...)
			return &c, nil
		}
	}
	return nil, tierlist.ErrNotFound
}
