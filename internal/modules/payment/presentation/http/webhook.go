package httpapi

import (
	"io"
	"net/http"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/httpx"
)

// This file is US2's transport: the public PayOS callback. It carries no session
// guard — the provider is not a user — so the signature is the only
// authentication, verified by the use case over the exact received bytes
// (FR-019, research D10). A handled event answers 2xx so PayOS stops retrying.

// PayOSWebhook receives a PayOS payment callback and applies it exactly once. It
// is public: no session is required, and the raw body is handed to the use case,
// which verifies the signature before trusting anything (FR-007, FR-019). A body
// whose signature does not verify answers 401 PAYMENT_WEBHOOK_INVALID_SIGNATURE;
// a body that does not parse answers 400 MALFORMED_REQUEST. A handled event —
// applied, already applied, recorded UNMATCHED, or a verified event for an unknown
// order code (acknowledged with nothing recorded) — answers 200 so the provider
// stops retrying (contracts/openapi.yaml, contracts/error-codes.md).
//
//	@Summary		PayOS payment webhook
//	@Description	Public provider callback. The signature is verified over the exact received body before anything is trusted; a handled event answers 2xx so PayOS stops retrying. An unverified signature applies nothing, an unknown order code is acknowledged with nothing recorded, and an amount mismatch is recorded for the operator.
//	@Tags			webhooks
//	@Accept			json
//	@Produce		json
//	@Param			request	body		object	true	"PayOS webhook envelope {code, desc, success, data, signature}"
//	@Success		200		{object}	httpx.SwaggerSuccess
//	@Failure		400		{object}	httpx.SwaggerError
//	@Failure		401		{object}	httpx.SwaggerError
//	@Failure		429		{object}	httpx.SwaggerError
//	@Failure		500		{object}	httpx.SwaggerError
//	@Router			/payments/payos/webhook [post]
func (h *Handler) PayOSWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		httpx.WriteError(w, r, httpx.New(httpx.CodeMalformedRequest), h.logger)
		return
	}
	if err := h.svc.HandleWebhook(r.Context(), body); err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteSuccess(w, r, http.StatusOK, nil)
}
