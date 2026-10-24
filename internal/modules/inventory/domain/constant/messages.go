package constant

// Client-facing messages paired with the error codes in codes.go.
//
// Each message is the default text presentation answers for its code. The
// code stays the stable API identifier; the message may be reworded without
// renaming the code. Branching on errors always uses the sentinel errors in
// domain/error with errors.Is/errors.As, never these strings.
const (
	// MsgInsufficientStock answers CodeInsufficientStock.
	MsgInsufficientStock = "Insufficient stock"
)
