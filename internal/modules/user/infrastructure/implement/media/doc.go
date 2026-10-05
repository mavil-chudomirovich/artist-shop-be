// Package media implements the MediaStore port against the external media
// service. The provider's credentials come from configuration and MUST NOT be
// logged.
//
// It currently holds only the fail-closed placeholder (Unavailable): the real
// provider adapter arrives with task T048, and until then every call fails with
// the module's retryable media error instead of pretending to succeed.
package media
