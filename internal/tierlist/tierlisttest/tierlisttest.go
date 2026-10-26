// Package tierlisttest provides in-memory fakes of the tierlist dependencies
// for unit tests of the service and the HTTP handlers.
package tierlisttest

import (
	"context"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/basili4-1982/tierl/internal/domain"
	"github.com/basili4-1982/tierl/internal/similarity"
	"github.com/basili4-1982/tierl/internal/tierlist"
)

// MemStore is an in-memory tierlist.Store.
type MemStore struct {
	mu          sync.Mutex
	byID        map[string]domain.Tierlist
	subs        map[string]int
	submissions map[string]*memSubmission

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

// ListPublic implements tierlist.Store; the fake has no catalog.
func (m *MemStore) ListPublic(context.Context, tierlist.PageParams) (tierlist.CatalogPage, error) {
	return tierlist.CatalogPage{}, nil
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
		if now.After(existing.CreatedAt) {
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
