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

// Register starts registration and emails an OTP.
//
//	@Summary		Register a new account
//	@Description	Creates a pending account and emails a one-time confirmation code. The response never reveals whether the email already exists.
//	@Tags			Auth
//	@Accept			json
//	@Produce		json
//	@Param			request	body		httpdto.RegisterRequest	true	"Registration payload"
//	@Success		202		{object}	httpx.SwaggerSuccess{data=httpdto.MessageResponse}
//	@Failure		400		{object}	httpx.SwaggerError
//	@Failure		429		{object}	httpx.SwaggerError
//	@Router			/auth/register [post]
func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	var req httpdto.RegisterRequest
	if err := decode(r, &req); err != nil {
		h.fail(w, r, httpx.New(httpx.CodeMalformedRequest))
		return
	}
	if err := h.svc.Register(r.Context(), appdto.RegisterInput{Email: req.Email, Password: req.Password}); err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteSuccess(w, r, http.StatusAccepted, httpdto.MessageResponse{Message: "If the email is eligible, a confirmation code has been sent."})
}

// VerifyEmail confirms the OTP.
//
//	@Summary		Confirm an email address
//	@Description	Confirms a pending account with the one-time code sent at registration.
//	@Tags			Auth
//	@Accept			json
//	@Produce		json
//	@Param			request	body		httpdto.VerifyEmailRequest	true	"Email and confirmation code"
//	@Success		200		{object}	httpx.SwaggerSuccess{data=httpdto.MessageResponse}
//	@Failure		400		{object}	httpx.SwaggerError
//	@Failure		429		{object}	httpx.SwaggerError
//	@Router			/auth/verify-email [post]
func (h *Handler) VerifyEmail(w http.ResponseWriter, r *http.Request) {
	var req httpdto.VerifyEmailRequest
	if err := decode(r, &req); err != nil {
		h.fail(w, r, httpx.New(httpx.CodeMalformedRequest))
		return
	}
	if err := h.svc.VerifyEmail(r.Context(), appdto.VerifyEmailInput{Email: req.Email, OTP: req.OTP}); err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteSuccess(w, r, http.StatusOK, httpdto.MessageResponse{Message: "Email confirmed."})
}

// ResendVerification re-sends the OTP.
//
//	@Summary		Resend the confirmation code
//	@Description	Re-sends the confirmation code if the email is eligible. The response is identical either way.
//	@Tags			Auth
//	@Accept			json
//	@Produce		json
//	@Param			request	body		httpdto.EmailRequest	true	"Email address"
//	@Success		202		{object}	httpx.SwaggerSuccess{data=httpdto.MessageResponse}
//	@Failure		400		{object}	httpx.SwaggerError
//	@Failure		429		{object}	httpx.SwaggerError
//	@Router			/auth/resend-verification [post]
func (h *Handler) ResendVerification(w http.ResponseWriter, r *http.Request) {
	var req httpdto.EmailRequest
	if err := decode(r, &req); err != nil {
		h.fail(w, r, httpx.New(httpx.CodeMalformedRequest))
		return
	}
	if err := h.svc.ResendVerification(r.Context(), appdto.EmailInput{Email: req.Email}); err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteSuccess(w, r, http.StatusAccepted, httpdto.MessageResponse{Message: "If the email is eligible, a confirmation code has been sent."})
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
		Email:     req.Email,
		Password:  req.Password,
		Source:    clientIP(r),
		UserAgent: r.UserAgent(),
		IP:        clientIP(r),
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

// ForgotPassword requests a reset link.
//
//	@Summary		Request a password reset
//	@Description	Sends a reset link if the email is registered. The response is identical either way.
//	@Tags			Auth
//	@Accept			json
//	@Produce		json
//	@Param			request	body		httpdto.EmailRequest	true	"Email address"
//	@Success		202		{object}	httpx.SwaggerSuccess{data=httpdto.MessageResponse}
//	@Failure		400		{object}	httpx.SwaggerError
//	@Failure		429		{object}	httpx.SwaggerError
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
	httpx.WriteSuccess(w, r, http.StatusAccepted, httpdto.MessageResponse{Message: "If the email is registered, a reset link has been sent."})
}

// ResetPassword completes a reset.
//
//	@Summary		Complete a password reset
//	@Description	Sets a new password from a reset token and returns a fresh session.
//	@Tags			Auth
//	@Accept			json
//	@Produce		json
//	@Param			request	body		httpdto.ResetRequest	true	"Reset token and new password"
//	@Success		200		{object}	httpx.SwaggerSuccess{data=httpdto.SessionResponse}
//	@Failure		400		{object}	httpx.SwaggerError
//	@Failure		401		{object}	httpx.SwaggerError
//	@Router			/auth/password/reset [post]
func (h *Handler) ResetPassword(w http.ResponseWriter, r *http.Request) {
	var req httpdto.ResetRequest
	if err := decode(r, &req); err != nil {
		h.fail(w, r, httpx.New(httpx.CodeMalformedRequest))
		return
	}
	session, err := h.svc.ResetPassword(r.Context(), appdto.ResetPasswordInput{Token: req.Token, NewPassword: req.NewPassword})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteSuccess(w, r, http.StatusOK, toSessionResponse(session))
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
	httpx.WriteSuccess(w, r, http.StatusOK, httpdto.MessageResponse{Message: "admin"})
}
