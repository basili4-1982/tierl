package tierlist

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/basili4-1982/tierl/internal/domain"
)

// Service errors. Use errors.Is to test for them.
var (
	// ErrForbidden is returned when the caller is not the author of the tier list.
	ErrForbidden = errors.New("tierlist: forbidden")
	// ErrEmailNotVerified is returned when a user without a confirmed email creates a tier list.
	ErrEmailNotVerified = errors.New("tierlist: email not verified")
)

// Profile is the part of a user account the service needs.
type Profile struct {
	DisplayName   string
	EmailVerified bool
}

// Users resolves user accounts. The auth module implements it.
type Users interface {
	// Profile returns the profile of the user or ErrNotFound.
	Profile(ctx context.Context, userID string) (Profile, error)
}

// ServiceDeps are the injected side effects of the Service.
type ServiceDeps struct {
	// Now returns the current time; defaults to time.Now.
	Now func() time.Time
	// Rand is the entropy source for IDs and slugs; defaults to crypto/rand.Reader.
	Rand io.Reader
	// Answers persists participant answers. When nil, a store that implements
	// Answers is used automatically.
	Answers Answers
	// Lookup reads stored answers by result ID or session. When nil, a store
	// that implements SubmissionLookup is used automatically.
	Lookup SubmissionLookup
	// ReportHideThreshold is the number of reports after which a public tier
	// list is hidden automatically. Zero uses DefaultReportHideThreshold.
	ReportHideThreshold int
	// HideReportsThreshold is an alias for ReportHideThreshold kept for
	// callers that name the setting after the resulting action.
	HideReportsThreshold int
	// ReportThreshold and ReportsThreshold are aliases kept for callers that
	// use the shorter names.
	ReportThreshold  int
	ReportsThreshold int
}

// Service holds the business logic of tier list management: creation,
// reading, editing, closing and deleting by the author.
type Service struct {
	store           Store
	users           Users
	answers         Answers
	lookup          SubmissionLookup
	now             func() time.Time
	rand            io.Reader
	reportThreshold int
}

// NewService builds a Service on top of the given store and user directory.
func NewService(store Store, users Users, deps ServiceDeps) *Service {
	s := &Service{store: store, users: users, now: deps.Now, rand: deps.Rand}
	s.reportThreshold = deps.ReportHideThreshold
	for _, n := range []int{deps.HideReportsThreshold, deps.ReportThreshold, deps.ReportsThreshold} {
		if s.reportThreshold <= 0 {
			s.reportThreshold = n
		}
	}
	if s.reportThreshold <= 0 {
		s.reportThreshold = DefaultReportHideThreshold
	}
	if deps.Answers != nil {
		s.answers = deps.Answers
	} else if answers, ok := store.(Answers); ok {
		s.answers = answers
	}
	if deps.Lookup != nil {
		s.lookup = deps.Lookup
	} else if lookup, ok := store.(SubmissionLookup); ok {
		s.lookup = lookup
	}
	if s.now == nil {
		s.now = time.Now
	}
	if s.rand == nil {
		s.rand = rand.Reader
	}
	return s
}

// TierInput is a tier supplied by the author; its position is its index.
type TierInput struct {
	Name  string
	Color string
}

// ItemInput is an item supplied by the author; its position is its index.
type ItemInput struct {
	Label    string
	ImageURL string
}

// CreateInput is the input of Service.Create.
type CreateInput struct {
	Title       string
	Description string
	Visibility  domain.Visibility
	// Slug is required for public tier lists and ignored for unlisted ones.
	Slug  string
	Tiers []TierInput
	Items []ItemInput
}

// UpdateInput is the input of Service.Update. Nil fields stay unchanged.
type UpdateInput struct {
	Title       *string
	Description *string
	Visibility  *domain.Visibility
	// Status closes or reopens the tier list (open/closed only).
	Status *domain.Status
	// Tiers and Items replace the whole set; forbidden after the first answer.
	Tiers []TierInput
	Items []ItemInput
}

// MyTierlist is one row of the author's profile list.
type MyTierlist struct {
	ID               string
	Slug             string
	Title            string
	Description      string
	Visibility       domain.Visibility
	Status           domain.Status
	ItemCount        int
	SubmissionsCount int
	CreatedAt        time.Time
	AuthorID         string
	AuthorName       string
}

// Details is a tier list prepared for the author or a reader.
type Details struct {
	Tierlist         *domain.Tierlist
	AuthorName       string
	IsOwner          bool
	SubmissionsCount int
}

// ClosedAt returns the moment the tier list was closed, if it is closed.
// The domain does not keep a separate timestamp: closing is the last change.
func (d *Details) ClosedAt() *time.Time {
	if d.Tierlist.Status != domain.StatusClosed {
		return nil
	}
	t := d.Tierlist.UpdatedAt
	return &t
}

// Create validates the input and stores a new published (open) tier list of
// the user.
func (s *Service) Create(ctx context.Context, userID string, in CreateInput) (*Details, error) {
	prof, err := s.users.Profile(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("tierlist: load author: %w", err)
	}
	if !prof.EmailVerified {
		return nil, ErrEmailNotVerified
	}

	id, err := s.newID()
	if err != nil {
		return nil, err
	}
	items, tiers, err := s.buildStructure(in.Items, in.Tiers)
	if err != nil {
		return nil, err
	}
	t, err := domain.NewTierlist(domain.NewTierlistParams{
		ID:          id,
		AuthorID:    userID,
		Title:       in.Title,
		Description: in.Description,
		Visibility:  in.Visibility,
		Slug:        in.Slug,
		Items:       items,
		Tiers:       tiers,
	}, domain.Deps{Now: s.now, Rand: s.rand})
	if err != nil {
		return nil, err
	}
	if err := t.Publish(s.now()); err != nil {
		return nil, err
	}
	if err := s.store.Create(ctx, t); err != nil {
		return nil, err
	}
	return &Details{Tierlist: t, AuthorName: prof.DisplayName, IsOwner: true}, nil
}

// Get returns a tier list by slug. viewerID is empty for anonymous readers.
// Draft and hidden tier lists are reported as not found.
func (s *Service) Get(ctx context.Context, viewerID, slug string) (*Details, error) {
	t, err := s.readable(ctx, slug)
	if err != nil {
		return nil, err
	}
	return s.details(ctx, t, viewerID)
}

// ListMine returns the tier lists of userID with their state and aggregates.
// A foreign tier list never appears in the result.
func (s *Service) ListMine(ctx context.Context, userID string) ([]MyTierlist, error) {
	if userID == "" {
		return nil, ErrForbidden
	}
	ownerStore, ok := s.store.(OwnerStore)
	if !ok {
		return nil, errors.New("tierlist: store does not support owner lists")
	}
	entries, err := ownerStore.ListByOwner(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("tierlist: list mine: %w", err)
	}
	name := ""
	if s.users != nil {
		prof, err := s.users.Profile(ctx, userID)
		switch {
		case err == nil:
			name = prof.DisplayName
		case errors.Is(err, ErrNotFound):
			// A removed account: keep the rows without a display name.
		default:
			return nil, fmt.Errorf("tierlist: load author: %w", err)
		}
	}
	out := make([]MyTierlist, 0, len(entries))
	for _, e := range entries {
		out = append(out, MyTierlist{
			ID: e.ID, Slug: e.Slug, Title: e.Title, Description: e.Description,
			Visibility: e.Visibility, Status: e.Status, ItemCount: e.ItemCount,
			SubmissionsCount: e.Submissions, CreatedAt: e.CreatedAt,
			AuthorID: e.AuthorID, AuthorName: name,
		})
	}
	return out, nil
}

// Update changes a tier list of its author. Title, description and visibility
// can always change; items and tiers only until the first answer
// (domain.ErrStructureLocked afterwards). Status closes or reopens the list.
func (s *Service) Update(ctx context.Context, userID, slug string, in UpdateInput) (*Details, error) {
	t, err := s.owned(ctx, userID, slug)
	if err != nil {
		return nil, err
	}
	now := s.now()

	if in.Title != nil || in.Description != nil {
		title, desc := t.Title, t.Description
		if in.Title != nil {
			title = *in.Title
		}
		if in.Description != nil {
			desc = *in.Description
		}
		if err := t.UpdateText(title, desc, now); err != nil {
			return nil, err
		}
	}
	if in.Visibility != nil {
		if err := s.changeVisibility(t, *in.Visibility, now); err != nil {
			return nil, err
		}
	}
	if in.Status != nil {
		if err := changeStatus(t, *in.Status, now); err != nil {
			return nil, err
		}
	} else if in.Visibility != nil && *in.Visibility == domain.VisibilityPublic && t.Status == domain.StatusClosed {
		// "public" is the author-facing name of the reopened state; accepting it
		// as visibility on a closed list makes reopening work for both clients.
		if err := changeStatus(t, domain.StatusOpen, now); err != nil {
			return nil, err
		}
	}
	if in.Items != nil || in.Tiers != nil {
		if err := s.changeStructure(ctx, t, in, now); err != nil {
			return nil, err
		}
	}

	if err := s.store.Update(ctx, t); err != nil {
		return nil, err
	}
	return s.details(ctx, t, userID)
}

func (s *Service) changeVisibility(t *domain.Tierlist, v domain.Visibility, now time.Time) error {
	if !v.Valid() {
		return &domain.ValidationError{Field: "visibility", Reason: "must be public or unlisted"}
	}
	if v == t.Visibility {
		return nil
	}
	// An unlisted link must be unguessable: a short readable slug is replaced.
	if v == domain.VisibilityUnlisted && len(t.Slug) < domain.MinUnlistedSlugLength {
		slug, err := domain.GenerateSlug(s.rand)
		if err != nil {
			return err
		}
		t.Slug = slug
	}
	t.Visibility, t.UpdatedAt = v, now
	return nil
}

func (s *Service) changeStructure(ctx context.Context, t *domain.Tierlist, in UpdateInput, now time.Time) error {
	items, tiers := t.Items, t.Tiers
	newItems, newTiers, err := s.buildStructure(in.Items, in.Tiers)
	if err != nil {
		return err
	}
	if in.Items != nil {
		items = newItems
	}
	if in.Tiers != nil {
		tiers = newTiers
	}
	if sameStructure(t.Items, items, t.Tiers, tiers) {
		return nil // nothing really changes, so the lock does not apply
	}

	n, err := s.store.CountSubmissions(ctx, t.ID)
	if err != nil {
		return err
	}
	return t.ReplaceStructure(items, tiers, n, now)
}

// Close closes the tier list of its author for new answers. It is idempotent.
func (s *Service) Close(ctx context.Context, userID, slug string) (*Details, error) {
	t, err := s.owned(ctx, userID, slug)
	if err != nil {
		return nil, err
	}
	if t.Status != domain.StatusClosed {
		now := s.now()
		if err := changeStatus(t, domain.StatusClosed, now); err != nil {
			return nil, err
		}
		if err := s.setStatus(ctx, t, userID, domain.StatusClosed, now); err != nil {
			return nil, err
		}
	}
	return s.details(ctx, t, userID)
}

// Delete removes the tier list of its author together with its answers.
func (s *Service) Delete(ctx context.Context, userID, ref string) error {
	t, err := s.owned(ctx, userID, ref)
	if err != nil {
		return err
	}
	if owner, ok := s.store.(OwnerStore); ok {
		return owner.DeleteForOwner(ctx, t.ID, userID)
	}
	return s.store.Delete(ctx, t.ID)
}

// changeStatus applies the author's open/closed transition. Closing and
// reopening are idempotent.
func changeStatus(t *domain.Tierlist, target domain.Status, now time.Time) error {
	switch target {
	case domain.StatusClosed:
		switch t.Status {
		case domain.StatusClosed:
			return nil
		case domain.StatusOpen:
			t.Status, t.UpdatedAt = domain.StatusClosed, now
			return nil
		}
	case domain.StatusOpen:
		switch t.Status {
		case domain.StatusOpen:
			return nil
		case domain.StatusClosed, domain.StatusDraft:
			t.Status, t.UpdatedAt = domain.StatusOpen, now
			return nil
		}
	}
	return fmt.Errorf("%w: %s -> %s", domain.ErrInvalidTransition, t.Status, target)
}

func (s *Service) setStatus(ctx context.Context, t *domain.Tierlist, userID string, status domain.Status, now time.Time) error {
	if owner, ok := s.store.(OwnerStore); ok {
		return owner.SetStatusForOwner(ctx, t.ID, userID, status, now)
	}
	return s.store.SetStatus(ctx, t.ID, status, now)
}

// readable loads a tier list that readers may see.
func (s *Service) readable(ctx context.Context, slug string) (*domain.Tierlist, error) {
	if err := domain.ValidateSlug(slug, domain.VisibilityPublic); err != nil {
		return nil, ErrNotFound
	}
	t, err := s.store.GetBySlug(ctx, slug)
	if err != nil {
		return nil, err
	}
	if t.Status != domain.StatusOpen && t.Status != domain.StatusClosed {
		return nil, ErrNotFound
	}
	return t, nil
}

// owned loads a tier list by ID (or, for the legacy slug routes, by slug) and
// checks that userID is its author. A foreign list is reported as not found so
// its existence is not leaked.
func (s *Service) owned(ctx context.Context, userID, ref string) (*domain.Tierlist, error) {
	if userID == "" {
		return nil, ErrForbidden
	}
	t, err := s.store.GetByID(ctx, ref)
	if errors.Is(err, ErrNotFound) {
		// Legacy routes address a tier list by slug and keep the reader
		// visibility rules (draft/hidden stay not found).
		t, err = s.readable(ctx, ref)
	}
	if err != nil {
		return nil, err
	}
	if t.AuthorID != userID {
		return nil, ErrNotFound
	}
	return t, nil
}

func (s *Service) details(ctx context.Context, t *domain.Tierlist, viewerID string) (*Details, error) {
	n, err := s.store.CountSubmissions(ctx, t.ID)
	if err != nil {
		return nil, err
	}
	d := &Details{Tierlist: t, SubmissionsCount: n, IsOwner: viewerID != "" && viewerID == t.AuthorID}
	prof, err := s.users.Profile(ctx, t.AuthorID)
	switch {
	case err == nil:
		d.AuthorName = prof.DisplayName
	case errors.Is(err, ErrNotFound):
		// A removed account: the tier list stays readable without a name.
	default:
		return nil, fmt.Errorf("tierlist: load author: %w", err)
	}
	return d, nil
}

// buildStructure turns author input into domain items and tiers with fresh IDs.
// An unset (nil) side is returned empty.
func (s *Service) buildStructure(items []ItemInput, tiers []TierInput) ([]domain.Item, []domain.Tier, error) {
	var outItems []domain.Item
	for i, it := range items {
		if err := validateImageURL(it.ImageURL); err != nil {
			return nil, nil, err
		}
		id, err := s.newID()
		if err != nil {
			return nil, nil, err
		}
		outItems = append(outItems, domain.Item{ID: id, Label: it.Label, ImageURL: it.ImageURL, Position: i})
	}
	var outTiers []domain.Tier
	for i, tr := range tiers {
		id, err := s.newID()
		if err != nil {
			return nil, nil, err
		}
		outTiers = append(outTiers, domain.Tier{ID: id, Name: tr.Name, Color: tr.Color, Position: i})
	}
	return outItems, outTiers, nil
}

// validateImageURL accepts an empty value, an absolute http(s) URL, or a
// root-relative path served by the configured media storage (for example
// /media/users/<id>/<hash>.png).
func validateImageURL(raw string) error {
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return &domain.ValidationError{Field: "item.image_url", Reason: "must be an absolute http(s) URL or a root-relative path"}
	}
	if u.Scheme == "http" || u.Scheme == "https" {
		if u.Host == "" {
			return &domain.ValidationError{Field: "item.image_url", Reason: "must be an absolute http(s) URL or a root-relative path"}
		}
		return nil
	}
	if u.Scheme == "" && u.Host == "" && strings.HasPrefix(raw, "/") && !strings.HasPrefix(raw, "//") {
		return nil
	}
	return &domain.ValidationError{Field: "item.image_url", Reason: "must be an absolute http(s) URL or a root-relative path"}
}

// sameStructure compares the editable content of items and tiers, ignoring IDs.
func sameStructure(oldItems, newItems []domain.Item, oldTiers, newTiers []domain.Tier) bool {
	return slices.EqualFunc(oldItems, newItems, func(a, b domain.Item) bool {
		return a.Label == b.Label && a.ImageURL == b.ImageURL && a.Position == b.Position
	}) && slices.EqualFunc(oldTiers, newTiers, func(a, b domain.Tier) bool {
		return a.Name == b.Name && strings.EqualFold(a.Color, b.Color) && a.Position == b.Position
	})
}

// newID returns a random UUID version 4.
func (s *Service) newID() (string, error) {
	var b [16]byte
	if _, err := io.ReadFull(s.rand, b[:]); err != nil {
		return "", fmt.Errorf("%w: %w", domain.ErrRandom, err)
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}
