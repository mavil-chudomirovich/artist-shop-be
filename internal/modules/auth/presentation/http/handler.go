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
func (h *Handler) AdminProbe(w http.ResponseWriter, r *http.Request) {
	httpx.WriteSuccess(w, r, http.StatusOK, httpdto.MessageResponse{Message: "admin"})
}
