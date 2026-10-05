// Package media implements the MediaStore port against the external media
// service. The provider's credentials come from configuration and MUST NOT be
// logged.
//
// The adapter speaks Cloudinary's signed REST API with the standard library, so
// no SDK is added to this binary for two endpoints (ADR-005). Every provider
// failure is flattened to the module's retryable media error with no provider
// detail attached, and a missing configuration fails closed rather than pretending
// to succeed — which is what makes absent media settings disable avatar upload and
// nothing else.
//
// Flattening leaves a caller nothing to diagnose, so the adapter also logs its own
// sanitised classification of a provider failure: the operation, and a sentence it
// wrote itself. No provider prose, endpoint, payload or credential is logged.
package media
