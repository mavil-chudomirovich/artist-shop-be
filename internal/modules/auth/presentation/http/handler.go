// Package httpapi exposes the auth module over HTTP.
package httpapi

import (
	"encoding/json"
	"log/slog"
	"net"
	"net/http"

	"github.com/google/uuid"

	appdto "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/application/dto"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/application/interface"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/error"
	httpdto "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/presentation/dto"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/httpx"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/middleware"
)

// Handler adapts the auth use cases to HTTP.
type Handler struct {
	svc     appinterface.AuthService
	auditor appinterface.Auditor
	logger  *slog.Logger
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	httpx.WriteError(w, r, mapError(err), h.logger)
}

func decode(r *http.Request, dst any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func toSessionResponse(s appdto.SessionOutput) httpdto.SessionResponse {
	return httpdto.SessionResponse{AccessToken: s.AccessToken, RefreshToken: s.RefreshToken, ExpiresIn: s.ExpiresIn}
}

// StartRegistration starts registration and emails an OTP.
//
//	@Summary		Start registration
//	@Description	Validates the payload, rejects an email or username already taken, holds the registration temporarily and emails a one-time code. No account is created until the code is confirmed.
//	@Tags			Auth
//	@Accept			json
//	@Produce		json
//	@Param			request	body		httpdto.StartRegistrationRequest	true	"Registration payload"
//	@Success		202		{object}	httpx.SwaggerSuccess{data=httpdto.RegistrationStartedResponse}
//	@Failure		400		{object}	httpx.SwaggerError
//	@Failure		409		{object}	httpx.SwaggerError
//	@Failure		429		{object}	httpx.SwaggerError
//	@Failure		503		{object}	httpx.SwaggerError
//	@Router			/auth/register [post]
func (h *Handler) StartRegistration(w http.ResponseWriter, r *http.Request) {
	var req httpdto.StartRegistrationRequest
	if err := decode(r, &req); err != nil {
		h.fail(w, r, httpx.New(httpx.CodeMalformedRequest))
		return
	}
	out, err := h.svc.StartRegistration(r.Context(), appdto.StartRegistrationInput{
		Email:       req.Email,
		Username:    req.Username,
		DisplayName: req.DisplayName,
		Password:    req.Password,
	})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteSuccess(w, r, http.StatusAccepted, httpdto.RegistrationStartedResponse{
		RegistrationID:    out.RegistrationID,
		Status:            out.Status,
		OTPExpiresIn:      out.OTPExpiresIn,
		ResendAvailableIn: out.ResendAvailableIn,
	})
}

// VerifyRegistrationOTP confirms the code and creates the account.
//
//	@Summary		Confirm a registration code
//	@Description	Confirms the one-time code against the temporary registration and, on success, creates an active customer account. Repeating a successful confirmation does not create a second account. No session is issued.
//	@Tags			Auth
//	@Accept			json
//	@Produce		json
//	@Param			request	body		httpdto.VerifyRegistrationRequest	true	"Registration reference and code"
//	@Success		200		{object}	httpx.SwaggerSuccess{data=httpdto.MessageResponse}
//	@Failure		400		{object}	httpx.SwaggerError
//	@Failure		409		{object}	httpx.SwaggerError
//	@Failure		429		{object}	httpx.SwaggerError
//	@Router			/auth/register/verify-otp [post]
func (h *Handler) VerifyRegistrationOTP(w http.ResponseWriter, r *http.Request) {
	var req httpdto.VerifyRegistrationRequest
	if err := decode(r, &req); err != nil {
		h.fail(w, r, httpx.New(httpx.CodeMalformedRequest))
		return
	}
	if err := h.svc.VerifyRegistrationOTP(r.Context(), appdto.VerifyRegistrationInput{RegistrationID: req.RegistrationID, OTP: req.OTP}); err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteSuccess(w, r, http.StatusOK, httpdto.MessageResponse{Code: constant.CodeRegistrationConfirmed, Message: constant.MsgRegistrationConfirmed})
}

// ResendRegistrationOTP replaces the confirmation code for a registration.
//
//	@Summary		Request a fresh confirmation code
//	@Description	Issues a new one-time code for a live registration after the resend cooldown, invalidating the previous code and resetting the attempt budget. Unknown, expired or completed references are answered generically.
//	@Tags			Auth
//	@Accept			json
//	@Produce		json
//	@Param			request	body		httpdto.RegistrationIDRequest	true	"Registration reference"
//	@Success		202		{object}	httpx.SwaggerSuccess{data=httpdto.MessageResponse}
//	@Failure		400		{object}	httpx.SwaggerError
//	@Failure		429		{object}	httpx.SwaggerError
//	@Failure		503		{object}	httpx.SwaggerError
//	@Router			/auth/register/resend-otp [post]
func (h *Handler) ResendRegistrationOTP(w http.ResponseWriter, r *http.Request) {
	var req httpdto.RegistrationIDRequest
	if err := decode(r, &req); err != nil {
		h.fail(w, r, httpx.New(httpx.CodeMalformedRequest))
		return
	}
	if err := h.svc.ResendRegistrationOTP(r.Context(), appdto.RegistrationIDInput{RegistrationID: req.RegistrationID}); err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteSuccess(w, r, http.StatusAccepted, httpdto.MessageResponse{Code: constant.CodeCodeSent, Message: constant.MsgCodeSent})
}

// CancelRegistration discards a temporary registration.
//
//	@Summary		Cancel a registration
//	@Description	Removes a temporary registration and invalidates its reference and code. The response is idempotent and identical for an unknown, expired or cancelled reference.
//	@Tags			Auth
//	@Accept			json
//	@Produce		json
//	@Param			request	body		httpdto.RegistrationIDRequest	true	"Registration reference"
//	@Success		200		{object}	httpx.SwaggerSuccess{data=httpdto.MessageResponse}
//	@Failure		400		{object}	httpx.SwaggerError
//	@Failure		429		{object}	httpx.SwaggerError
//	@Router			/auth/register/cancel [post]
func (h *Handler) CancelRegistration(w http.ResponseWriter, r *http.Request) {
	var req httpdto.RegistrationIDRequest
	if err := decode(r, &req); err != nil {
		h.fail(w, r, httpx.New(httpx.CodeMalformedRequest))
		return
	}
	if err := h.svc.CancelRegistration(r.Context(), appdto.RegistrationIDInput{RegistrationID: req.RegistrationID}); err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteSuccess(w, r, http.StatusOK, httpdto.MessageResponse{Code: constant.CodeRegistrationCancelled, Message: constant.MsgRegistrationCancelled})
}

// Login authenticates and issues tokens.
//
//	@Summary		Log in
//	@Description	Authenticates an account and returns an access/refresh token pair.
//	@Tags			Auth
//	@Accept			json
//	@Produce		json
//	@Param			request	body		httpdto.LoginRequest	true	"Email and password"
//	@Success		200		{object}	httpx.SwaggerSuccess{data=httpdto.SessionResponse}
//	@Failure		400		{object}	httpx.SwaggerError
//	@Failure		401		{object}	httpx.SwaggerError
//	@Failure		403		{object}	httpx.SwaggerError
//	@Failure		429		{object}	httpx.SwaggerError
//	@Router			/auth/login [post]
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var req httpdto.LoginRequest
	if err := decode(r, &req); err != nil {
		h.fail(w, r, httpx.New(httpx.CodeMalformedRequest))
		return
	}
	session, err := h.svc.Login(r.Context(), appdto.LoginInput{
		Email:      req.Email,
		Identifier: req.Identifier,
		Password:   req.Password,
		Source:     clientIP(r),
		UserAgent:  r.UserAgent(),
		IP:         clientIP(r),
	})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteSuccess(w, r, http.StatusOK, toSessionResponse(session))
}

// Refresh rotates a session.
//
//	@Summary		Refresh a session
//	@Description	Rotates a refresh token and returns a new token pair.
//	@Tags			Auth
//	@Accept			json
//	@Produce		json
//	@Param			request	body		httpdto.TokenRequest	true	"Refresh token"
//	@Success		200		{object}	httpx.SwaggerSuccess{data=httpdto.SessionResponse}
//	@Failure		400		{object}	httpx.SwaggerError
//	@Failure		401		{object}	httpx.SwaggerError
//	@Router			/auth/refresh [post]
func (h *Handler) Refresh(w http.ResponseWriter, r *http.Request) {
	var req httpdto.TokenRequest
	if err := decode(r, &req); err != nil {
		h.fail(w, r, httpx.New(httpx.CodeMalformedRequest))
		return
	}
	session, err := h.svc.Refresh(r.Context(), appdto.RefreshInput{RefreshToken: req.RefreshToken, UserAgent: r.UserAgent(), IP: clientIP(r)})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteSuccess(w, r, http.StatusOK, toSessionResponse(session))
}

// Logout revokes a session.
//
//	@Summary		Log out
//	@Description	Revokes the given refresh token's session.
//	@Tags			Auth
//	@Accept			json
//	@Produce		json
//	@Param			request	body	httpdto.TokenRequest	true	"Refresh token"
//	@Success		204		"No Content"
//	@Failure		400		{object}	httpx.SwaggerError
//	@Router			/auth/logout [post]
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	var req httpdto.TokenRequest
	if err := decode(r, &req); err != nil {
		h.fail(w, r, httpx.New(httpx.CodeMalformedRequest))
		return
	}
	if err := h.svc.Logout(r.Context(), appdto.RefreshInput{RefreshToken: req.RefreshToken}); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ForgotPassword requests a reset code.
//
//	@Summary		Request a password-reset code
//	@Description	Sends a one-time code by email if the address is registered. The response is identical either way, so it never reveals whether an account exists. The email carries the code only, never the reset token.
//	@Tags			Auth
//	@Accept			json
//	@Produce		json
//	@Param			request	body		httpdto.EmailRequest	true	"Email address"
//	@Success		202		{object}	httpx.SwaggerSuccess{data=httpdto.MessageResponse}
//	@Failure		400		{object}	httpx.SwaggerError
//	@Failure		429		{object}	httpx.SwaggerError
//	@Failure		503		{object}	httpx.SwaggerError
//	@Router			/auth/password/forgot [post]
func (h *Handler) ForgotPassword(w http.ResponseWriter, r *http.Request) {
	var req httpdto.EmailRequest
	if err := decode(r, &req); err != nil {
		h.fail(w, r, httpx.New(httpx.CodeMalformedRequest))
		return
	}
	if err := h.svc.ForgotPassword(r.Context(), appdto.EmailInput{Email: req.Email}); err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteSuccess(w, r, http.StatusAccepted, httpdto.MessageResponse{Code: constant.CodeCodeSent, Message: constant.MsgCodeSent})
}

// VerifyResetOTP verifies the emailed reset code and returns a reset token.
//
//	@Summary		Verify the password-reset code
//	@Description	Verifies the emailed one-time code and, on success, issues a single-use reset token whose only purpose is to set a new password. The token is not a session and cannot authenticate a request; the code can no longer be used afterwards.
//	@Tags			Auth
//	@Accept			json
//	@Produce		json
//	@Param			request	body		httpdto.VerifyResetOTPRequest	true	"Email address and code"
//	@Success		200		{object}	httpx.SwaggerSuccess{data=httpdto.ResetTokenResponse}
//	@Failure		400		{object}	httpx.SwaggerError
//	@Failure		429		{object}	httpx.SwaggerError
//	@Router			/auth/password/forgot/verify-otp [post]
func (h *Handler) VerifyResetOTP(w http.ResponseWriter, r *http.Request) {
	var req httpdto.VerifyResetOTPRequest
	if err := decode(r, &req); err != nil {
		h.fail(w, r, httpx.New(httpx.CodeMalformedRequest))
		return
	}
	out, err := h.svc.VerifyResetOTP(r.Context(), appdto.VerifyResetOTPInput{Email: req.Email, OTP: req.OTP})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteSuccess(w, r, http.StatusOK, httpdto.ResetTokenResponse{ResetToken: out.ResetToken, ExpiresIn: out.ExpiresIn})
}

// ResetPassword completes a reset.
//
//	@Summary		Complete a password reset
//	@Description	Sets a new password from a single-use reset token, consumes the token, ends every session of the account and rejects previously issued access tokens. Returns success only; it does not sign the user in.
//	@Tags			Auth
//	@Accept			json
//	@Produce		json
//	@Param			request	body		httpdto.ResetRequest	true	"Reset token and new password"
//	@Success		200		{object}	httpx.SwaggerSuccess{data=httpdto.MessageResponse}
//	@Failure		400		{object}	httpx.SwaggerError
//	@Failure		429		{object}	httpx.SwaggerError
//	@Router			/auth/password/reset [post]
func (h *Handler) ResetPassword(w http.ResponseWriter, r *http.Request) {
	var req httpdto.ResetRequest
	if err := decode(r, &req); err != nil {
		h.fail(w, r, httpx.New(httpx.CodeMalformedRequest))
		return
	}
	if err := h.svc.ResetPassword(r.Context(), appdto.ResetPasswordInput{Token: req.Token, NewPassword: req.NewPassword}); err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteSuccess(w, r, http.StatusOK, httpdto.MessageResponse{Code: constant.CodePasswordReset, Message: constant.MsgPasswordReset})
}

// ChangePassword changes the password of the signed-in account.
//
//	@Summary		Change the password
//	@Description	Changes the signed-in account's password and rotates its sessions. Requires the current password and refresh token.
//	@Tags			Auth
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			request	body		httpdto.ChangePasswordRequest	true	"Current password, new password and refresh token"
//	@Success		200		{object}	httpx.SwaggerSuccess{data=httpdto.SessionResponse}
//	@Failure		400		{object}	httpx.SwaggerError
//	@Failure		401		{object}	httpx.SwaggerError
//	@Router			/auth/password/change [post]
func (h *Handler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	identity, ok := middleware.IdentityFromContext(r.Context())
	if !ok {
		h.fail(w, r, domainerr.ErrInvalidToken)
		return
	}
	accountID, err := uuid.Parse(identity.Subject)
	if err != nil {
		h.fail(w, r, domainerr.ErrInvalidToken)
		return
	}
	var req httpdto.ChangePasswordRequest
	if err := decode(r, &req); err != nil {
		h.fail(w, r, httpx.New(httpx.CodeMalformedRequest))
		return
	}
	if req.RefreshToken == "" {
		httpx.WriteError(w, r, httpx.NewWithDetails(httpx.CodeValidation, httpx.Detail{
			Field: "refreshToken",
			Issue: "the current refresh token is required",
		}), h.logger)
		return
	}
	session, err := h.svc.ChangePassword(r.Context(), appdto.ChangePasswordInput{
		AccountID:           accountID,
		CurrentPassword:     req.CurrentPassword,
		NewPassword:         req.NewPassword,
		CurrentRefreshToken: req.RefreshToken,
		CurrentAccessJTI:    identity.TokenID,
		UserAgent:           r.UserAgent(),
		IP:                  clientIP(r),
	})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteSuccess(w, r, http.StatusOK, toSessionResponse(session))
}

// Me returns the current identity.
//
//	@Summary		Current identity
//	@Description	Returns the identity of the account behind the access token.
//	@Tags			Auth
//	@Produce		json
//	@Security		BearerAuth
//	@Success		200	{object}	httpx.SwaggerSuccess{data=httpdto.IdentityResponse}
//	@Failure		401	{object}	httpx.SwaggerError
//	@Router			/auth/me [get]
func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	identity, ok := middleware.IdentityFromContext(r.Context())
	if !ok {
		h.fail(w, r, domainerr.ErrInvalidToken)
		return
	}
	accountID, err := uuid.Parse(identity.Subject)
	if err != nil {
		h.fail(w, r, domainerr.ErrInvalidToken)
		return
	}
	out, err := h.svc.Identity(r.Context(), accountID)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteSuccess(w, r, http.StatusOK, httpdto.IdentityResponse{ID: identity.Subject, Email: out.Email, Role: string(out.Role)})
}

// AdminProbe is an admin-only endpoint used to verify RBAC.
//
//	@Summary		Admin RBAC probe
//	@Description	Returns a fixed payload and is reachable only by an ADMIN account. A denial is recorded in the audit log.
//	@Tags			Auth
//	@Produce		json
//	@Security		BearerAuth
//	@Success		200	{object}	httpx.SwaggerSuccess{data=httpdto.MessageResponse}
//	@Failure		401	{object}	httpx.SwaggerError
//	@Failure		403	{object}	httpx.SwaggerError
//	@Router			/auth/admin/probe [get]
func (h *Handler) AdminProbe(w http.ResponseWriter, r *http.Request) {
	httpx.WriteSuccess(w, r, http.StatusOK, httpdto.MessageResponse{Code: constant.CodeAdminProbe, Message: constant.MsgAdminProbe})
}
