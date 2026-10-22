// Package tierlisttest provides in-memory fakes of the tierlist dependencies
// for unit tests of the service and the HTTP handlers.
package tierlisttest

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/basili4-1982/tierl/internal/domain"
	"github.com/basili4-1982/tierl/internal/tierlist"
)

// MemStore is an in-memory tierlist.Store.
type MemStore struct {
	mu   sync.Mutex
	byID map[string]domain.Tierlist
	subs map[string]int

	// Errs makes the named method (for example "Create") fail with the error.
	Errs map[string]error
}

var _ tierlist.Store = (*MemStore)(nil)

// NewMemStore returns an empty store.
func NewMemStore() *MemStore {
	return &MemStore{byID: map[string]domain.Tierlist{}, subs: map[string]int{}, Errs: map[string]error{}}
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
	if m.subs[t.ID] > 0 && (!slices.Equal(cur.Items, t.Items) || !slices.Equal(cur.Tiers, t.Tiers)) {
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
	return m.subs[id], nil
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
