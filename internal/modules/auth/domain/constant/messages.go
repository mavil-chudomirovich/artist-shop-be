package constant

// Client-facing messages paired with the error codes in codes.go.
//
// Each message is the default text presentation answers for its code. The
// code stays the stable API identifier; the message may be reworded without
// renaming the code. Branching on errors always uses the sentinel errors in
// domain/error with errors.Is/errors.As, never these strings.
//
// Success acknowledgements below pair with the success codes in codes.go. A
// handler answers them through MessageResponse and must reference these
// constants instead of string literals.
const (
	// MsgTooManyConfirmationAttempts answers CodeOTPTooManyAttempts. It covers
	// both the exhausted-attempts and the blocked-OTP refusals, which share one
	// client-facing code on purpose.
	MsgTooManyConfirmationAttempts = "Too many confirmation attempts"

	// MsgRegistrationConfirmed answers CodeRegistrationConfirmed.
	MsgRegistrationConfirmed = "Registration confirmed."
	// MsgCodeSent answers CodeCodeSent on both the registration resend and the
	// password-reset request flows.
	MsgCodeSent = "A confirmation code has been sent."
	// MsgRegistrationCancelled answers CodeRegistrationCancelled.
	MsgRegistrationCancelled = "Registration cancelled."
	// MsgPasswordReset answers CodePasswordReset.
	MsgPasswordReset = "Password reset."
	// MsgAdminProbe answers CodeAdminProbe.
	MsgAdminProbe = "admin"
)
