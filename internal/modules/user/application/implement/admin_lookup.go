package implement

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/contracts"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/application/interface"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/audit"
)

// lookupAddressWindow is the address window the operator lookup reads.
//
// Unlike the customer's own listing this answer is not paginated: FR-007d requires
// a consumer to be offered every address with the default first, and the contract
// DTO carries no window. The window is still bounded, by the same ceiling the
// customer's listing uses, so a single request can never ask the database for an
// unbounded result.
const lookupAddressWindow = maxAddressPageSize

// The module satisfies the cross-module contract with its own service rather than
// through a second façade: the composition root wires *implement.Service wherever
// internal/contracts.CustomerLookupService is wanted, and there is no second
// implementation of the lookup that could answer differently from the route
// (Constitution I, docs/system-design/contract-purity.md, research D8).
var _ contracts.CustomerLookupService = (*Service)(nil)

// The whole use-case surface is satisfied now that the operator lookup exists, so
// the compile-time assertion lands here rather than in an earlier file.
var _ appinterface.UserService = (*Service)(nil)

// LookupCustomer returns one customer's contact details and non-hidden addresses
// for an operator.
//
// The account is the *subject* of the read, never the actor: it arrives as the path
// parameter of a route that is role-guarded, while the acting administrator comes
// from the session that presentation placed in the context. Nothing a client can
// send names who is asking (FR-006, research D5).
//
// The answer is read-only. This module offers no write path for customer data to an
// administrator — no route, no use case, and no repository method reachable from
// here — which is what FR-022 asks for and what research D8 settled: every attempt
// to change those details is simply not available rather than refused later.
//
// Every successful read records USER_PROFILE_VIEWED_BY_ADMIN: customer contact
// data leaves the customer's control only with that trace (FR-022a,
// Constitution VI). A read that found nothing is not audited: nothing left the
// customer's control, and claiming otherwise would fill the trail with events an
// auditor has to filter out to see the real ones.
func (s *Service) LookupCustomer(ctx context.Context, userID uuid.UUID) (contracts.Customer, error) {
	profile, err := s.Profiles.ByID(ctx, userID)
	if err != nil {
		if !errors.Is(err, domainerr.ErrUserNotFound) {
			// A storage failure is not "no such customer": translating it into the
			// contract's not-found sentinel would tell a consumer the account does not
			// exist when the truth is that the database could not answer.
			return contracts.Customer{}, err
		}
		// The contract sentinel, not the module's own error, so a consumer can branch
		// on it without importing this module (internal/contracts/user.go).
		return contracts.Customer{}, fmt.Errorf("%w: %w", contracts.ErrCustomerNotFound, err)
	}

	// The repository orders this listing default address first, and that order is the
	// guarantee FR-007d rests on: the mapping below preserves it rather than
	// re-sorting, so there is one ordering rule in the module instead of two that
	// could disagree.
	list, _, err := s.Addresses.ListByOwner(ctx, userID, 1, lookupAddressWindow)
	if err != nil {
		return contracts.Customer{}, err
	}

	s.Audit.Record(ctx, constant.AuditProfileViewedByAdmin, string(audit.OutcomeSuccess),
		auditActor(ctx), auditActorRole(ctx), targetTypeProfile, profile.ID.String(),
		// The event counts the addresses that were read; it never copies the contact
		// data itself into the trail (Constitution VI).
		map[string]any{"addressCount": len(list)},
	)
	return s.Mapper.Customer(*profile, list), nil
}

// auditActor returns the acting account of the request for the audit row.
//
// It reports nil when the call is not serving a request — another module reading a
// customer through the contract has no operator session. A missing actor is a fact
// worth recording rather than a reason to skip the event, so the row is written
// either way.
func auditActor(ctx context.Context) *uuid.UUID {
	actor, ok := appinterface.ActorFromContext(ctx)
	if !ok {
		return nil
	}
	id := actor.ID
	return &id
}

// auditActorRole returns the acting account's role for the audit row, or an empty
// string when there is no session behind the call.
func auditActorRole(ctx context.Context) string {
	actor, ok := appinterface.ActorFromContext(ctx)
	if !ok {
		return ""
	}
	return string(actor.Role)
}
