package httpapi

import (
	"errors"
	"net/http"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/httpx"
)

func coded(code string, status int, message string) *httpx.AppError {
	return &httpx.AppError{Code: httpx.ErrorCode(code), Status: status, Message: message}
}

func mapError(err error) *httpx.AppError {
	switch {
	case errors.Is(err, domainerr.ErrWeakPassword):
		return coded(constant.CodeWeakPassword, http.StatusBadRequest, "Password does not meet the policy")
	case errors.Is(err, domainerr.ErrInvalidCredentials):
		return coded(constant.CodeInvalidCredentials, http.StatusUnauthorized, "Invalid email or password")
	case errors.Is(err, domainerr.ErrAccountPending):
		return coded(constant.CodeAccountPending, http.StatusForbidden, "Email confirmation required")
	case errors.Is(err, domainerr.ErrAccountDisabled):
		return coded(constant.CodeAccountDisabled, http.StatusForbidden, "Account is disabled")
	case errors.Is(err, domainerr.ErrOTPInvalid):
		return coded(constant.CodeOTPInvalid, http.StatusBadRequest, "Invalid confirmation code")
	case errors.Is(err, domainerr.ErrOTPExpired):
		return coded(constant.CodeOTPExpired, http.StatusBadRequest, "Confirmation code expired")
	case errors.Is(err, domainerr.ErrOTPTooManyAttempts):
		return coded(constant.CodeOTPTooManyAttempts, http.StatusTooManyRequests, "Too many confirmation attempts")
	case errors.Is(err, domainerr.ErrOTPBlocked):
		return coded(constant.CodeOTPTooManyAttempts, http.StatusTooManyRequests, "Too many confirmation attempts")
	case errors.Is(err, domainerr.ErrResendCooldown):
		return coded(constant.CodeResendCooldown, http.StatusTooManyRequests, "Please wait before requesting another code")
	case errors.Is(err, domainerr.ErrAccountLocked):
		return coded(constant.CodeLoginLocked, http.StatusTooManyRequests, "Too many failed sign-in attempts")
	case errors.Is(err, domainerr.ErrExpiredToken):
		return coded(constant.CodeTokenExpired, http.StatusUnauthorized, "Token expired")
	case errors.Is(err, domainerr.ErrInvalidToken), errors.Is(err, domainerr.ErrSessionNotFound):
		return coded(constant.CodeTokenInvalid, http.StatusUnauthorized, "Invalid token")
	case errors.Is(err, domainerr.ErrRefreshReused):
		return coded(constant.CodeRefreshReused, http.StatusUnauthorized, "Refresh token already used")
	case errors.Is(err, domainerr.ErrResetInvalid), errors.Is(err, domainerr.ErrResetNotFound):
		return coded(constant.CodeResetInvalid, http.StatusBadRequest, "Invalid or expired reset token")
	case errors.Is(err, domainerr.ErrVerificationDeliveryFailed):
		// A message the provider refused is a service problem, not a client
		// mistake, so it answers 503 rather than 500 (FR-006). The answer itself
		// carries the next step: nothing was delivered to the address and a new
		// code can be requested, which is true because a failed delivery does not
		// consume the resend cooldown (FR-024). The shared code is reused, so no
		// new client-facing code is added, and the cause is dropped here so no
		// provider wording, credential or recipient address can reach the client
		// (FR-009).
		return coded(string(httpx.CodeUnavailable), http.StatusServiceUnavailable,
			"The confirmation email could not be sent. Nothing was delivered to your address; request a new confirmation code and try again.")
	default:
		return httpx.Wrap(err, httpx.CodeInternal)
	}
}
