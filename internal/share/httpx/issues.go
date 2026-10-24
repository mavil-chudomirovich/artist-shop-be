package httpx

// Shared validation issue details.
//
// These issues describe request-shape problems the client fixes on its own
// (a malformed identifier, an out-of-range page window). They carry the same
// concept and meaning in every module, so they live here instead of being
// repeated as literals in each presentation layer (Constitution I). They are
// identification-free text: branching on errors always uses sentinel errors
// with errors.Is/errors.As, never these strings.
const (
	// IssueMustBeUUID reports a member that is not a valid UUID.
	IssueMustBeUUID = "must be a UUID"
	// IssueIntAtLeast1 reports a page number below the first page.
	IssueIntAtLeast1 = "must be an integer of at least 1"
	// IssueIntBetween1And100 reports a page size outside the allowed window.
	IssueIntBetween1And100 = "must be an integer between 1 and 100"
)
