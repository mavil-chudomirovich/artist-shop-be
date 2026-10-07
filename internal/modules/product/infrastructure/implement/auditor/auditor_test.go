package auditor

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/constant"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/audit"
)

// capturingEmitter keeps the emitted event so a test can assert the mapping onto
// the shared audit contract.
type capturingEmitter struct {
	event audit.Event
}

func (c *capturingEmitter) Emit(_ context.Context, e audit.Event) { c.event = e }

var _ audit.Emitter = (*capturingEmitter)(nil)

// The adapter's whole job is to translate the module's Record call onto the
// shared audit event, carrying the actor, the target and the outcome unchanged.
func TestRecordCarriesActorTargetAndOutcome(t *testing.T) {
	emitter := &capturingEmitter{}
	actor := uuid.New()

	New(emitter).Record(context.Background(), constant.AuditProductCreated, string(audit.OutcomeSuccess),
		&actor, "ADMIN", "product", uuid.New().String(), map[string]any{"slug": "tranh-son-dau"})

	if emitter.event.Action != "PRODUCT_CREATED" {
		t.Fatalf("unexpected action %q", emitter.event.Action)
	}
	if emitter.event.Outcome != audit.OutcomeSuccess {
		t.Fatalf("unexpected outcome %q", emitter.event.Outcome)
	}
	if emitter.event.ActorID == nil || *emitter.event.ActorID != actor {
		t.Fatal("expected the actor to be recorded")
	}
	if emitter.event.ActorRole != "ADMIN" || emitter.event.TargetType != "product" || emitter.event.TargetID == "" {
		t.Fatalf("unexpected target: %+v", emitter.event)
	}
	if emitter.event.Metadata["slug"] != "tranh-son-dau" {
		t.Fatalf("unexpected metadata: %+v", emitter.event.Metadata)
	}
}

// An anonymous actor is valid: the emitter must not invent one.
func TestRecordAllowsAnAnonymousActor(t *testing.T) {
	emitter := &capturingEmitter{}

	New(emitter).Record(context.Background(), constant.AuditProductDeleted, string(audit.OutcomeFailure),
		nil, "", "product", uuid.New().String(), nil)

	if emitter.event.ActorID != nil {
		t.Fatal("expected no actor for an anonymous action")
	}
	if emitter.event.Outcome != audit.OutcomeFailure {
		t.Fatalf("expected FAILURE, got %q", emitter.event.Outcome)
	}
}
