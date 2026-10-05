package middleware

import (
	"context"
	"errors"
	"net/http"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/httpx"
)

// Identity is the authenticated principal as resolved by the auth module.
// Role values are owned by the calling module's domain layer; the foundation
// does not define business roles.
type Identity struct {
	Subject string
	Role    string
	TokenID string
}

// AuthHooks are supplied by the auth module (module 01). The foundation defines
// the pipeline and enforcement points only and MUST NOT implement credential or
// session logic (FR-012).
type AuthHooks struct {
	// Authenticate resolves the caller identity from the request. Returning a
	// nil identity means the request is unauthenticated; returning an error is
	// treated as an authentication failure. An *httpx.AppError is passed
	// through unchanged so a module can expose its own token error codes.
	Authenticate func(ctx context.Context, r *http.Request) (*Identity, error)
	// Authorize authorizes an authenticated identity for the request.
	Authorize func(ctx context.Context, id Identity, r *http.Request) error
	// OnDenied is called before a role check answers FORBIDDEN, letting the
	// providing module record the privilege denial (FR-014).
	OnDenied func(ctx context.Context, id Identity, r *http.Request)
}

type identityKey struct{}

// IdentityFromContext returns the identity placed by Authentication.
func IdentityFromContext(ctx context.Context) (Identity, bool) {
	id, ok := ctx.Value(identityKey{}).(Identity)
	return id, ok
}

// Authentication resolves the caller identity when possible and stores it in the
// context. It never blocks the request; use RequireAuthentication to enforce.
func Authentication(hooks AuthHooks) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if hooks.Authenticate != nil {
				if id, err := hooks.Authenticate(r.Context(), r); err == nil && id != nil {
					r = r.WithContext(context.WithValue(r.Context(), identityKey{}, *id))
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireAuthentication rejects unauthenticated requests with UNAUTHENTICATED.
func RequireAuthentication(hooks AuthHooks) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			var id *Identity
			if hooks.Authenticate != nil {
				resolved, err := hooks.Authenticate(ctx, r)
				if err != nil {
					var appErr *httpx.AppError
					if errors.As(err, &appErr) {
						httpx.WriteError(w, r, appErr, nil)
						return
					}
					httpx.WriteError(w, r, httpx.Wrap(err, httpx.CodeUnauthenticated), nil)
					return
				}
				id = resolved
			}
			if id == nil {
				httpx.WriteError(w, r, httpx.New(httpx.CodeUnauthenticated), nil)
				return
			}
			r = r.WithContext(context.WithValue(ctx, identityKey{}, *id))
			next.ServeHTTP(w, r)
		})
	}
}

// RequireRole enforces that the authenticated identity holds the given role
// (FR-012, FR-014). The role value comes from the caller's domain layer.
func RequireRole(hooks AuthHooks, role string) func(http.Handler) http.Handler {
	requireAuth := RequireAuthentication(hooks)
	return func(next http.Handler) http.Handler {
		return requireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id, _ := IdentityFromContext(r.Context())
			if id.Role != role {
				if hooks.OnDenied != nil {
					hooks.OnDenied(r.Context(), id, r)
				}
				httpx.WriteError(w, r, httpx.New(httpx.CodeForbidden), nil)
				return
			}
			if hooks.Authorize != nil {
				if err := hooks.Authorize(r.Context(), id, r); err != nil {
					httpx.WriteError(w, r, httpx.Wrap(err, httpx.CodeForbidden), nil)
					return
				}
			}
			next.ServeHTTP(w, r)
		}))
	}
}
