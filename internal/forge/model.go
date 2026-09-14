package forge

import (
	"context"
	"time"
)

// ItemType distinguishes issues from change requests (PR/MR).
type ItemType string

const (
	ItemTypeIssue         ItemType = "issue"
	ItemTypeChangeRequest ItemType = "pr"
	// ItemTypePipeline is a synthetic item type for CI runs, which have no
	// issue/PR identity of their own. It exists so a pipeline event can travel
	// the same dispatch path as other events, which is item-shaped.
	ItemTypePipeline ItemType = "pipeline"
)

// State is the normalized lifecycle state of an item.
type State string

const (
	StateOpen   State = "open"
	StateClosed State = "closed"
	StateMerged State = "merged"
)

// Author is the minimal identity of a forge user.
type Author struct {
	Login string `json:"login"`
	Name  string `json:"name,omitempty"`
}

// Label is a forge label.
type Label struct {
	Name  string `json:"name"`
	Color string `json:"color,omitempty"`
}

// Item is the unified representation of an issue or change request, used by
// both the list and detail views regardless of the backing platform.
type Item struct {
	// Platform identifies the backing forge.
	Platform Platform `json:"platform"`
	// Type is issue or pr.
	Type ItemType `json:"type"`
	// Number is the platform-assigned issue/PR number.
	Number int `json:"number"`
	// Title is the item title.
	Title string `json:"title"`
	// Body is the raw Markdown body.
	Body string `json:"body,omitempty"`
	// State is the normalized state.
	State State `json:"state"`
	// Draft indicates a draft PR/MR (change requests only).
	Draft bool `json:"draft,omitempty"`
	// Author is the item creator.
	Author Author `json:"author"`
	// Assignees lists the users assigned to the item.
	Assignees []Author `json:"assignees,omitempty"`
	// Labels lists the item's labels.
	Labels []Label `json:"labels,omitempty"`
	// CommentCount is the number of comments on the item.
	CommentCount int `json:"commentCount"`
	// URL is the web URL of the item.
	URL string `json:"url"`
	// CreatedAt / UpdatedAt are the platform timestamps.
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	// MergedAt is set for merged change requests.
	MergedAt *time.Time `json:"mergedAt,omitempty"`
}

// Comment is a single comment on an issue or change request.
type Comment struct {
	// ID is the platform-assigned comment id. GitHub ids are globally
	// increasing; GitLab note ids are per-project. Never assume ordering
	// across platforms.
	ID int64 `json:"id"`
	// Author is the comment author.
	Author Author `json:"author"`
	// Body is the raw Markdown body.
	Body string `json:"body"`
	// CreatedAt / UpdatedAt are the platform timestamps. UpdatedAt changes when
	// a comment is edited even though ID does not — event detection relies on
	// both.
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// ListOptions controls a paginated list request.
type ListOptions struct {
	// Type selects issues or change requests.
	Type ItemType
	// State filters by lifecycle state: "open", "closed", or "all".
	State string
	// Page is the 1-based page number.
	Page int
	// PerPage is the page size (platform-clamped).
	PerPage int
	// Query is an optional server-side search string.
	Query string
	// Since, when non-zero, restricts results to items updated at or after it
	// (used by the incremental change fetcher).
	Since time.Time
	// Sort is the field to sort by ("updated" or "created"); empty uses the
	// platform default.
	Sort string
	// Direction is "asc" or "desc".
	Direction string
}

// ListResult is a page of items plus pagination metadata.
type ListResult struct {
	Items []Item
	// HasMore reports whether further pages exist.
	HasMore bool
	// NextPage is the next page number when HasMore is true.
	NextPage int
}

// Provider is the read-only forge surface. Implementations must never issue
// write requests.
type Provider interface {
	// CurrentUser returns the account the configured credential authenticates
	// as, used for the "assigned to me" filter.
	CurrentUser(ctx context.Context) (Author, error)

	// ListItems returns a page of issues or change requests.
	ListItems(ctx context.Context, opts ListOptions) (ListResult, error)

	// GetItem returns a single issue or change request by number.
	GetItem(ctx context.Context, typ ItemType, number int) (Item, error)

	// ListComments returns a page of comments for an item, oldest first.
	ListComments(ctx context.Context, typ ItemType, number, page, perPage int) ([]Comment, error)
}
