// Package constant holds the category module's business constants.
package constant

// Stable, machine-readable error codes of the category module. They mirror
// specs/005-category-catalog/contracts/error-codes.md one for one and are mapped
// to HTTP status codes by presentation/http/errors.go. Clients branch on
// error.code, never on error.message.
//
// A code is part of the module's public contract: renaming one breaks every
// client that already handles it.
const (
	// CodeCategoryNotFound covers an unknown identifier, a withheld category
	// and a removed one. On the public surface the three are deliberately one
	// answer, so the response cannot be used to discover hidden categories
	// (FR-005).
	CodeCategoryNotFound = "CATEGORY_NOT_FOUND"
	// CodeCategoryNameTaken reports that another category already uses the
	// name, ignoring letter case and surrounding whitespace (FR-016).
	CodeCategoryNameTaken = "CATEGORY_NAME_TAKEN"
	// CodeCategorySlugTaken reports that another category already uses the
	// slug (FR-017).
	CodeCategorySlugTaken = "CATEGORY_SLUG_TAKEN"
	// CodeCategoryInUse reports that a category cannot be removed because
	// products still belong to it. The reference is enforced by a restricting
	// foreign key added by feature 006, so the refusal comes from the storage
	// layer rather than from a check (FR-036); the answer tells the operator
	// what stands in the way (FR-037).
	CodeCategoryInUse = "CATEGORY_IN_USE"
)
