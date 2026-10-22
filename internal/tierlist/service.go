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
}

// Service holds the business logic of tier list management: creation,
// reading, editing, closing and deleting by the author.
type Service struct {
	store Store
	users Users
	now   func() time.Time
	rand  io.Reader
}

// NewService builds a Service on top of the given store and user directory.
func NewService(store Store, users Users, deps ServiceDeps) *Service {
	s := &Service{store: store, users: users, now: deps.Now, rand: deps.Rand}
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
	// Tiers and Items replace the whole set; forbidden after the first answer.
	Tiers []TierInput
	Items []ItemInput
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

// Update changes a tier list of its author. Title, description and visibility
// can always change; items and tiers only until the first answer
// (domain.ErrStructureLocked afterwards).
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
		if err := t.Close(now); err != nil {
			return nil, err
		}
		if err := s.store.SetStatus(ctx, t.ID, domain.StatusClosed, now); err != nil {
			return nil, err
		}
	}
	return s.details(ctx, t, userID)
}

// Delete removes the tier list of its author together with its answers.
func (s *Service) Delete(ctx context.Context, userID, slug string) error {
	t, err := s.owned(ctx, userID, slug)
	if err != nil {
		return err
	}
	return s.store.Delete(ctx, t.ID)
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

// owned loads a tier list and checks that userID is its author.
func (s *Service) owned(ctx context.Context, userID, slug string) (*domain.Tierlist, error) {
	t, err := s.readable(ctx, slug)
	if err != nil {
		return nil, err
	}
	if userID == "" || t.AuthorID != userID {
		return nil, ErrForbidden
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

// validateImageURL accepts an empty value or an absolute http(s) URL.
func validateImageURL(raw string) error {
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return &domain.ValidationError{Field: "item.image_url", Reason: "must be an absolute http or https URL"}
	}
	return nil
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
