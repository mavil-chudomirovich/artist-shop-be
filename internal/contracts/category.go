package contracts

import (
	"context"

	"github.com/google/uuid"
)

// CategoryQuery answers the two facts about categories that module 04 needs and
// cannot read itself: which categories a customer is allowed to see, and which
// category a customer-facing slug names.
//
// A product is visible only when its category is (FR-002), and that filter must
// run inside the product query rather than in Go, or a paginated list would
// return short pages and a total that counts rows the caller never receives
// (FR-006). Module 04 therefore needs module 03's display state in SQL, and
// Constitution I forbids it reading module 03's table. This interface is the
// arrangement the constitution prescribes for exactly that: the category module
// supplies an adapter at the composition root, and no module reaches into
// another's tables.
//
// It carries two questions rather than one because the public list's optional
// `category=<slug>` filter cannot be built from a set of identifiers alone: it
// needs the identifier a slug names. Both are facts about categories that module
// 04 cannot read, and both must reach the SQL for the same reason, so they belong
// to one dependency rather than two (research D1). The interface is named for the
// asking rather than for visibility alone, because a set of visible identifiers
// cannot answer what a slug names.
type CategoryQuery interface {
	// VisibleCategoryIDs returns the identifiers of every category whose
	// is_visible is true. The order is not part of the contract: the caller
	// uses the values as a set in SQL. A catalogue with no visible category
	// answers an empty slice, not an error.
	//
	// It answers with the whole set rather than a per-identifier check because
	// the list needs the set to filter in SQL. One method that serves both the
	// list and the single-product read is one thing to implement, one thing to
	// fake and one thing to test (research D1).
	VisibleCategoryIDs(ctx context.Context) ([]uuid.UUID, error)

	// CategoryIDBySlug resolves the category a customer-facing slug names. It
	// reports whether any category carries that slug; a slug no category holds
	// answers (uuid.Nil, false, nil) rather than a not-found error, so the
	// caller can answer the public filter's empty list directly.
	//
	// The found-flag is deliberate (research D1): a hidden slug, an unknown slug
	// and a slug naming a category with nothing visible in it must all answer the
	// same empty list (FR-006), and a found-flag lets the use case do that
	// without translating a not-found error into a success. A *hidden* slug
	// resolves to its identifier and is then excluded by the visibility
	// predicate anyway, so the two paths agree without a special case.
	CategoryIDBySlug(ctx context.Context, slug string) (uuid.UUID, bool, error)
}
