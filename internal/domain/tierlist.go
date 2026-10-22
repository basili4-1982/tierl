package domain

import (
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// Limits from docs/SPEC.md section 3. These constants are the source of truth.
const (
	MinItems = 3
	MaxItems = 50
	MinTiers = 2
	MaxTiers = 10

	MaxTitleLen       = 120
	MaxDescriptionLen = 1000
	MaxItemLabelLen   = 80
	// MaxTierNameLen is not in the SPEC; see the PR notes.
	MaxTierNameLen = 40
	// MaxImageURLLen is not in the SPEC; see the PR notes.
	MaxImageURLLen = 2048
)

var colorRe = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// Visibility controls whether a tier list is listed in the catalog.
type Visibility string

// Visibility values.
const (
	VisibilityPublic   Visibility = "public"
	VisibilityUnlisted Visibility = "unlisted"
)

// Valid reports whether v is a known visibility.
func (v Visibility) Valid() bool {
	return v == VisibilityPublic || v == VisibilityUnlisted
}

// Status is the lifecycle state of a tier list.
type Status string

// Status values.
const (
	StatusDraft  Status = "draft"
	StatusOpen   Status = "open"
	StatusClosed Status = "closed"
	StatusHidden Status = "hidden"
)

// Valid reports whether s is a known status.
func (s Status) Valid() bool {
	switch s {
	case StatusDraft, StatusOpen, StatusClosed, StatusHidden:
		return true
	}
	return false
}

// Item is something participants place into a tier.
type Item struct {
	ID       string
	Label    string
	ImageURL string // optional
	Position int
}

// Tier is a ranking bucket. Position 0 is the highest tier.
type Tier struct {
	ID       string
	Name     string
	Color    string // #rrggbb
	Position int
}

// Tierlist is the aggregate root.
type Tierlist struct {
	ID          string
	Slug        string
	AuthorID    string
	Title       string
	Description string
	Visibility  Visibility
	Status      Status
	Items       []Item
	Tiers       []Tier
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Deps are the injected side effects used by constructors.
type Deps struct {
	// Now returns the current time.
	Now func() time.Time
	// Rand is the entropy source for slugs (crypto/rand.Reader in production).
	Rand io.Reader
}

// NewTierlistParams is the input for NewTierlist.
type NewTierlistParams struct {
	ID          string
	AuthorID    string
	Title       string
	Description string
	Visibility  Visibility
	// Slug is required for public tier lists (readable, chosen by the caller)
	// and ignored for unlisted ones, which always get a random slug.
	Slug  string
	Items []Item
	Tiers []Tier
}

// NewTierlist validates p and returns a draft tier list.
func NewTierlist(p NewTierlistParams, d Deps) (*Tierlist, error) {
	if strings.TrimSpace(p.ID) == "" {
		return nil, invalid("id", "must not be empty")
	}
	if strings.TrimSpace(p.AuthorID) == "" {
		return nil, invalid("author_id", "must not be empty")
	}
	if err := ValidateTitle(p.Title); err != nil {
		return nil, err
	}
	if err := ValidateDescription(p.Description); err != nil {
		return nil, err
	}
	if !p.Visibility.Valid() {
		return nil, invalid("visibility", "must be public or unlisted")
	}
	if err := ValidateStructure(p.Items, p.Tiers); err != nil {
		return nil, err
	}

	slug := p.Slug
	if p.Visibility == VisibilityUnlisted {
		var err error
		if slug, err = GenerateSlug(d.Rand); err != nil {
			return nil, fmt.Errorf("new tierlist: %w", err)
		}
	}
	if err := ValidateSlug(slug, p.Visibility); err != nil {
		return nil, err
	}

	now := d.Now()
	return &Tierlist{
		ID:          p.ID,
		Slug:        slug,
		AuthorID:    p.AuthorID,
		Title:       p.Title,
		Description: p.Description,
		Visibility:  p.Visibility,
		Status:      StatusDraft,
		Items:       append([]Item(nil), p.Items...),
		Tiers:       append([]Tier(nil), p.Tiers...),
		CreatedAt:   now,
		UpdatedAt:   now,
	}, nil
}

// ValidateTitle checks the title (required, up to MaxTitleLen runes).
func ValidateTitle(s string) error {
	if strings.TrimSpace(s) == "" {
		return invalid("title", "must not be empty")
	}
	if n := utf8.RuneCountInString(s); n > MaxTitleLen {
		return invalid("title", "length %d exceeds %d", n, MaxTitleLen)
	}
	return nil
}

// ValidateDescription checks the optional description.
func ValidateDescription(s string) error {
	if n := utf8.RuneCountInString(s); n > MaxDescriptionLen {
		return invalid("description", "length %d exceeds %d", n, MaxDescriptionLen)
	}
	return nil
}

// ValidateStructure checks counts, string lengths, and position uniqueness of
// items and tiers. Positions of each set must be exactly 0..n-1.
func ValidateStructure(items []Item, tiers []Tier) error {
	if n := len(items); n < MinItems || n > MaxItems {
		return invalid("items", "count %d must be %d..%d", n, MinItems, MaxItems)
	}
	if n := len(tiers); n < MinTiers || n > MaxTiers {
		return invalid("tiers", "count %d must be %d..%d", n, MinTiers, MaxTiers)
	}

	pos := make([]int, len(items))
	for i, it := range items {
		pos[i] = it.Position
		if err := it.validate(); err != nil {
			return fmt.Errorf("item %d: %w", i, err)
		}
	}
	if err := checkPositions("items", pos); err != nil {
		return err
	}

	pos = make([]int, len(tiers))
	for i, t := range tiers {
		pos[i] = t.Position
		if err := t.validate(); err != nil {
			return fmt.Errorf("tier %d: %w", i, err)
		}
	}
	return checkPositions("tiers", pos)
}

func (it Item) validate() error {
	if strings.TrimSpace(it.ID) == "" {
		return invalid("item.id", "must not be empty")
	}
	if strings.TrimSpace(it.Label) == "" {
		return invalid("item.label", "must not be empty")
	}
	if n := utf8.RuneCountInString(it.Label); n > MaxItemLabelLen {
		return invalid("item.label", "length %d exceeds %d", n, MaxItemLabelLen)
	}
	if len(it.ImageURL) > MaxImageURLLen {
		return invalid("item.image_url", "length exceeds %d", MaxImageURLLen)
	}
	return nil
}

func (t Tier) validate() error {
	if strings.TrimSpace(t.ID) == "" {
		return invalid("tier.id", "must not be empty")
	}
	if strings.TrimSpace(t.Name) == "" {
		return invalid("tier.name", "must not be empty")
	}
	if n := utf8.RuneCountInString(t.Name); n > MaxTierNameLen {
		return invalid("tier.name", "length %d exceeds %d", n, MaxTierNameLen)
	}
	if !colorRe.MatchString(t.Color) {
		return invalid("tier.color", "must look like #rrggbb")
	}
	return nil
}

func checkPositions(field string, pos []int) error {
	seen := make([]bool, len(pos))
	for _, p := range pos {
		if p < 0 || p >= len(pos) {
			return invalid(field, "position %d out of range 0..%d", p, len(pos)-1)
		}
		if seen[p] {
			return invalid(field, "duplicate position %d", p)
		}
		seen[p] = true
	}
	return nil
}

// Publish moves a draft to open.
func (t *Tierlist) Publish(now time.Time) error { return t.transition(StatusDraft, StatusOpen, now) }

// Close moves an open tier list to closed.
func (t *Tierlist) Close(now time.Time) error { return t.transition(StatusOpen, StatusClosed, now) }

// Hide hides a tier list by moderation from any non-hidden status.
func (t *Tierlist) Hide(now time.Time) error {
	if t.Status == StatusHidden {
		return fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, t.Status, StatusHidden)
	}
	t.Status, t.UpdatedAt = StatusHidden, now
	return nil
}

func (t *Tierlist) transition(from, to Status, now time.Time) error {
	if t.Status != from {
		return fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, t.Status, to)
	}
	t.Status, t.UpdatedAt = to, now
	return nil
}

// AcceptsSubmissions reports whether answers may be submitted.
func (t *Tierlist) AcceptsSubmissions() bool { return t.Status == StatusOpen }

// UpdateText changes the title and description, which stay editable after answers.
func (t *Tierlist) UpdateText(title, description string, now time.Time) error {
	if err := ValidateTitle(title); err != nil {
		return err
	}
	if err := ValidateDescription(description); err != nil {
		return err
	}
	t.Title, t.Description, t.UpdatedAt = title, description, now
	return nil
}

// ReplaceStructure replaces items and tiers; forbidden once submissions exist.
func (t *Tierlist) ReplaceStructure(items []Item, tiers []Tier, submissions int, now time.Time) error {
	if submissions > 0 {
		return ErrStructureLocked
	}
	if err := ValidateStructure(items, tiers); err != nil {
		return err
	}
	t.Items = append([]Item(nil), items...)
	t.Tiers = append([]Tier(nil), tiers...)
	t.UpdatedAt = now
	return nil
}
