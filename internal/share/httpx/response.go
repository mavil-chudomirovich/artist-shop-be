// Package httpx defines the HTTP response envelope and error contract shared by
// every endpoint (FR-006, FR-007, FR-008).
package httpx

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/logging"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/reqctx"
)

// Meta is the metadata block attached to every success response.
type Meta struct {
	RequestID string `json:"requestId"`
	Timestamp string `json:"timestamp"`
	Page      int    `json:"page,omitempty"`
	PageSize  int    `json:"pageSize,omitempty"`
	Total     int64  `json:"total,omitempty"`
}

type successResponse struct {
	Data any  `json:"data"`
	Meta Meta `json:"meta"`
}

// ErrorBody is the standard error payload.
type ErrorBody struct {
	Code      string   `json:"code"`
	Message   string   `json:"message"`
	Details   []Detail `json:"details,omitempty"`
	RequestID string   `json:"requestId"`
}

type errorResponse struct {
	Error ErrorBody `json:"error"`
}

// WriteSuccess writes a payload using the standard success envelope.
func WriteSuccess(w http.ResponseWriter, r *http.Request, status int, data any) {
	writeJSON(w, status, successResponse{
		Data: data,
		Meta: Meta{
			RequestID: reqctx.CorrelationID(r.Context()),
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		},
	})
}

// WriteSuccessList writes a paginated payload using the standard success envelope.
func WriteSuccessList(w http.ResponseWriter, r *http.Request, data any, page, pageSize int, total int64) {
	writeJSON(w, http.StatusOK, successResponse{
		Data: data,
		Meta: Meta{
			RequestID: reqctx.CorrelationID(r.Context()),
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Page:      page,
			PageSize:  pageSize,
			Total:     total,
		},
	})
}

// WriteError writes an error using the standard error envelope. Non-AppError
// values are mapped to INTERNAL_ERROR and their details are never exposed.
func WriteError(w http.ResponseWriter, r *http.Request, err error, logger *slog.Logger) {
	appErr := FromError(err)
	if appErr.Status >= http.StatusInternalServerError && logger != nil {
		logging.WithCorrelation(r.Context(), logger).ErrorContext(r.Context(), "request failed",
			slog.String("code", string(appErr.Code)),
			slog.String("error", err.Error()),
		)
	}
	writeJSON(w, appErr.Status, errorResponse{
		Error: ErrorBody{
			Code:      string(appErr.Code),
			Message:   appErr.Message,
			Details:   appErr.Details,
			RequestID: reqctx.CorrelationID(r.Context()),
		},
	})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
