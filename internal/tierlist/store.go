// Package tierlist declares the storage contracts used by tier list services.
// Interfaces live on the consumer side; internal/storage/postgres implements
// them, and the domain package stays free of SQL.
package tierlist

import (
	"context"
	"errors"
	"time"

	"github.com/basili4-1982/tierl/internal/domain"
	"github.com/basili4-1982/tierl/internal/similarity"
)

// Storage errors. Use errors.Is to test for them.
var (
	// ErrNotFound is returned when a tier list does not exist.
	ErrNotFound = errors.New("tierlist: not found")
	// ErrSlugTaken is returned when a slug is already used by another tier list.
	ErrSlugTaken = errors.New("tierlist: slug already taken")
	// ErrNotAcceptingSubmissions is returned when answers are sent to a tier list that is not open.
	ErrNotAcceptingSubmissions = errors.New("tierlist: not accepting submissions")
	// ErrAlreadySubmitted is returned when a session already has an answer and
	// the caller asked to create a new one instead of replacing it.
	ErrAlreadySubmitted = errors.New("tierlist: already submitted")
)

// Store persists tier lists with their items and tiers.
type Store interface {
	// Create inserts a new tier list with its items and tiers.
	// It returns ErrSlugTaken on a slug collision.
	Create(ctx context.Context, t *domain.Tierlist) error
	// GetBySlug returns the tier list with the given slug or ErrNotFound.
	GetBySlug(ctx context.Context, slug string) (*domain.Tierlist, error)
	// GetByID returns the tier list with the given ID or ErrNotFound.
	GetByID(ctx context.Context, id string) (*domain.Tierlist, error)
	// Update saves text, slug, visibility, status and, while there are no
	// submissions, items and tiers. It returns ErrNotFound, ErrSlugTaken, or
	// domain.ErrStructureLocked when items or tiers change after the first answer.
	Update(ctx context.Context, t *domain.Tierlist) error
	// SetStatus changes only the status (for example close or hide).
	SetStatus(ctx context.Context, id string, status domain.Status, now time.Time) error
	// Delete removes the tier list with all answers or returns ErrNotFound.
	Delete(ctx context.Context, id string) error
	// CountSubmissions returns the number of answers of a tier list.
	CountSubmissions(ctx context.Context, id string) (int, error)
	// ListPublic returns the public catalog: public and open tier lists,
	// newest first, with keyset pagination.
	ListPublic(ctx context.Context, p PageParams) (CatalogPage, error)
}

// Placement puts an item into a tier.
type Placement struct {
	ItemID string
	TierID string
}

// Submission is one anonymous answer.
type Submission struct {
	// ID is used for a new answer; a replaced answer keeps its original ID.
	ID         string
	TierlistID string
	// SessionID is the hash of the anonymous session token.
	SessionID  string
	Placements []Placement
}

// SaveResult describes the outcome of Answers.Save.
type SaveResult struct {
	// SubmissionID is the stored answer ID (the original one on replacement).
	SubmissionID string
	// Replaced is true when an earlier answer of the same session was overwritten.
	Replaced bool
}

// StoredSubmission is an answer loaded from the store. SessionID is the hash
// of the anonymous session token and never leaves the server.
type StoredSubmission struct {
	ID           string
	TierlistID   string
	TierlistSlug string
	SessionID    string
	Placements   []Placement
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// ItemDistribution is the number of answers per tier for one item.
type ItemDistribution struct {
	ItemID string
	// Counts is indexed by tier position; its length is the number of tiers.
	Counts []int
}

// Answers persists anonymous answers and their aggregates.
type Answers interface {
	// CreateAnswer stores the first answer of a session. It returns
	// ErrAlreadySubmitted when the session already answered, and updates the
	// per-tier counters in the same transaction otherwise. It also returns
	// ErrNotFound, ErrNotAcceptingSubmissions, or domain.ErrInvalid for
	// unknown items or tiers and duplicate items. (The name avoids clashing
	// with Store.Create, which creates a tier list.)
	CreateAnswer(ctx context.Context, s Submission, now time.Time) (SaveResult, error)
	// Save stores the answer of a session, replacing the earlier one, and
	// updates the per-tier counters in the same transaction. It returns
	// ErrNotFound, ErrNotAcceptingSubmissions, or domain.ErrInvalid for
	// unknown items or tiers and duplicate items.
	Save(ctx context.Context, s Submission, now time.Time) (SaveResult, error)
	// Answers returns every answer of a tier list as a similarity.Answer
	// (tier index = tier position), ordered by creation time.
	Answers(ctx context.Context, tierlistID string) ([]StoredAnswer, error)
	// Distributions returns the per-item tier counts from the pre-aggregate,
	// ordered by item position.
	Distributions(ctx context.Context, tierlistID string) ([]ItemDistribution, error)
	// GetSubmissionByID returns one answer by its ID or ErrNotFound.
	GetSubmissionByID(ctx context.Context, id string) (*StoredSubmission, error)
	// GetSubmissionBySession returns the answer of a session in a tier list or
	// ErrNotFound.
	GetSubmissionBySession(ctx context.Context, tierlistID, sessionID string) (*StoredSubmission, error)
	// ListSubmissionsBySession returns all answers of a session, newest change
	// first.
	ListSubmissionsBySession(ctx context.Context, sessionID string) ([]StoredSubmission, error)
}

// StoredAnswer is an answer with its ID, ready for the similarity package.
type StoredAnswer struct {
	ID     string
	Answer similarity.Answer
}

// PageParams selects a catalog page.
type PageParams struct {
	// Limit is the page size, 1..MaxPageSize; 0 means DefaultPageSize.
	Limit int
	// Cursor is the opaque value from the previous page; empty for the first page.
	Cursor string
}

// Page size bounds.
const (
	DefaultPageSize = 20
	MaxPageSize     = 100
)

// CatalogEntry is a tier list summary for the catalog (without items).
type CatalogEntry struct {
	ID          string
	Slug        string
	Title       string
	Description string
	ItemCount   int
	Submissions int
	CreatedAt   time.Time
}

// CatalogPage is one page of the catalog.
type CatalogPage struct {
	Entries []CatalogEntry
	// NextCursor is empty on the last page.
	NextCursor string
}

// ErrBadCursor is returned for a malformed catalog cursor.
var ErrBadCursor = errors.New("tierlist: invalid cursor")
