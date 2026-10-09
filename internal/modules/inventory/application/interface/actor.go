package appinterface

import (
	"context"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/access"
)

// Actor is the account a request is being served for, carried beside the context
// so a use case can name it in an audit event without importing a transport
// package (Constitution I).
//
// It is only ever filled from the authenticated session by presentation, which is
// what keeps the acting identity out of reach of the request: no client body,
// query string or path parameter can put a value here (FR-005).
type Actor struct {
	// ID is the acting account.
	ID uuid.UUID
	// Role is the role the session carries. The role comes from the token, so an
	// audit event can never name a privilege the caller did not hold.
	Role access.Role
}

type actorKey struct{}

// WithActor returns a context carrying the acting account of the request.
func WithActor(ctx context.Context, actor Actor) context.Context {
	return context.WithValue(ctx, actorKey{}, actor)
}

// ActorFromContext returns the acting account of the request. It reports false
// when there is no session behind the call.
func ActorFromContext(ctx context.Context) (Actor, bool) {
	actor, ok := ctx.Value(actorKey{}).(Actor)
	return actor, ok
}
