package httpx

import (
	"errors"
	"fmt"
	"net/http"
)

// ErrorCode is a stable, machine-readable error identifier.
type ErrorCode string

// Common error codes (contracts/error-codes.md).
const (
	CodeValidation       ErrorCode = "VALIDATION_ERROR"
	CodeMalformedRequest ErrorCode = "MALFORMED_REQUEST"
	CodeUnauthenticated  ErrorCode = "UNAUTHENTICATED"
	CodeForbidden        ErrorCode = "FORBIDDEN"
	CodeNotFound         ErrorCode = "NOT_FOUND"
	CodeMethodNotAllowed ErrorCode = "METHOD_NOT_ALLOWED"
	CodeConflict         ErrorCode = "CONFLICT"
	CodePayloadTooLarge  ErrorCode = "PAYLOAD_TOO_LARGE"
	CodeUnsupportedMedia ErrorCode = "UNSUPPORTED_MEDIA_TYPE"
	CodeRateLimited      ErrorCode = "RATE_LIMITED"
	CodeInternal         ErrorCode = "INTERNAL_ERROR"
	CodeUnavailable      ErrorCode = "SERVICE_UNAVAILABLE"
)

type definition struct {
	status  int
	message string
}

var catalogue = map[ErrorCode]definition{
	CodeValidation:       {http.StatusBadRequest, "Request validation failed"},
	CodeMalformedRequest: {http.StatusBadRequest, "Request could not be parsed"},
	CodeUnauthenticated:  {http.StatusUnauthorized, "Authentication required"},
	CodeForbidden:        {http.StatusForbidden, "Permission denied"},
	CodeNotFound:         {http.StatusNotFound, "Resource not found"},
	CodeMethodNotAllowed: {http.StatusMethodNotAllowed, "Method not allowed"},
	CodeConflict:         {http.StatusConflict, "Request conflicts with current state"},
	CodePayloadTooLarge:  {http.StatusRequestEntityTooLarge, "Request body is too large"},
	CodeUnsupportedMedia: {http.StatusUnsupportedMediaType, "Unsupported media type"},
	CodeRateLimited:      {http.StatusTooManyRequests, "Too many requests"},
	CodeInternal:         {http.StatusInternalServerError, "An unexpected error occurred"},
	CodeUnavailable:      {http.StatusServiceUnavailable, "Service is not ready"},
}

// Detail describes a single field-level or contextual problem.
type Detail struct {
	Field string `json:"field,omitempty"`
	Issue string `json:"issue,omitempty"`
}

// AppError is a typed application error safe to expose to clients.
type AppError struct {
	Code    ErrorCode
	Status  int
	Message string
	Details []Detail
	err     error
}

// Error implements the error interface.
func (e *AppError) Error() string {
	if e.err != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.err)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Unwrap exposes the wrapped cause for errors.Is / errors.As.
func (e *AppError) Unwrap() error { return e.err }

// New creates an AppError from the catalogue.
func New(code ErrorCode) *AppError {
	def, ok := catalogue[code]
	if !ok {
		def = catalogue[CodeInternal]
	}
	return &AppError{Code: code, Status: def.status, Message: def.message}
}

// NewWithDetails creates an AppError carrying field-level details.
func NewWithDetails(code ErrorCode, details ...Detail) *AppError {
	e := New(code)
	e.Details = details
	return e
}

// Wrap creates an AppError that wraps a cause for logging while exposing only
// the safe message to clients.
func Wrap(err error, code ErrorCode) *AppError {
	e := New(code)
	e.err = err
	return e
}

// FromError maps any error to an AppError, defaulting to INTERNAL_ERROR.
func FromError(err error) *AppError {
	if err == nil {
		return New(CodeInternal)
	}
	var appErr *AppError
	if errors.As(err, &appErr) {
		return appErr
	}
	wrapped := New(CodeInternal)
	wrapped.err = err
	return wrapped
}
