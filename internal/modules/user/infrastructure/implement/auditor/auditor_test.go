package auditor

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/domain/constant"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/audit"
)

// capturingEmitter keeps the emitted event so a test can assert the mapping onto
// the shared audit contract.
type capturingEmitter struct {
	event audit.Event
}

func (c *capturingEmitter) Emit(_ context.Context, e audit.Event) { c.event = e }

var _ audit.Emitter = (*capturingEmitter)(nil)

func TestRecordCarriesActorTargetAndOutcome(t *testing.T) {
	emitter := &capturingEmitter{}
	actor := uuid.New()

	New(emitter).Record(context.Background(), constant.AuditProfileViewedByAdmin, string(audit.OutcomeSuccess),
		&actor, "ADMIN", "user", uuid.New().String(), map[string]any{"ip": "127.0.0.1"})

	if emitter.event.Action != "USER_PROFILE_VIEWED_BY_ADMIN" {
		t.Fatalf("unexpected action %q", emitter.event.Action)
	}
	if emitter.event.Outcome != audit.OutcomeSuccess {
		t.Fatalf("unexpected outcome %q", emitter.event.Outcome)
	}
	if emitter.event.ActorID == nil || *emitter.event.ActorID != actor {
		t.Fatal("expected the actor to be recorded")
	}
	if emitter.event.ActorRole != "ADMIN" || emitter.event.TargetType != "user" || emitter.event.TargetID == "" {
		t.Fatalf("unexpected target: %+v", emitter.event)
	}
	if emitter.event.Metadata["ip"] != "127.0.0.1" {
		t.Fatalf("unexpected metadata: %+v", emitter.event.Metadata)
	}
}

func TestRecordAllowsAnAnonymousActor(t *testing.T) {
	emitter := &capturingEmitter{}

	New(emitter).Record(context.Background(), constant.AuditAddressDeleted, string(audit.OutcomeFailure),
		nil, "", "address", uuid.New().String(), nil)

	if emitter.event.ActorID != nil {
		t.Fatal("expected no actor for an anonymous action")
	}
	if emitter.event.Outcome != audit.OutcomeFailure {
		t.Fatalf("expected FAILURE, got %q", emitter.event.Outcome)
	}
}
