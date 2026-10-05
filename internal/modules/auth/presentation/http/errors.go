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
	case errors.Is(err, domainerr.ErrEmailTaken):
		return coded(constant.CodeEmailTaken, http.StatusConflict, "Email already registered")
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
	default:
		return httpx.Wrap(err, httpx.CodeInternal)
	}
}
