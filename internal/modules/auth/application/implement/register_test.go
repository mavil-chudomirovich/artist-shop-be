package implement

import (
	"context"
	"fmt"
	"strings"
	"testing"

	appdto "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/application/dto"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/error"
)

// registerPassword satisfies the policy, so a test that is not about the policy
// never fails on it.
const registerPassword = "Str0ng!Pass"

// configurationFailure is a refusal a retry cannot change, so a test spends
// exactly one delivery attempt on it (FR-016).
func configurationFailure() error {
	return fmt.Errorf("%w: the provider reported status 550", domainerr.ErrDeliveryConfiguration)
}

func registerInput(email string) appdto.RegisterInput {
	return appdto.RegisterInput{Email: email, Password: registerPassword}
}

// TestAFailedRegistrationDoesNotRevealWhetherTheAddressExisted states the
// property FR-008a requires, on its own: while a message cannot be delivered, the
// answer must be the same for an address that was already registered and one that
// was not, because a difference would let an unauthenticated caller enumerate
// registered addresses for as long as the outage lasts.
//
// Only an address that is still pending is compared against an unknown one. An
// address that is already verified is not sent a code at all, so its premise -
// a message that could not be delivered - never applies to it.
func TestAFailedRegistrationDoesNotRevealWhetherTheAddressExisted(t *testing.T) {
	const email = "oracle@example.com"

	// The address does not exist, so this request is the one that creates the
	// pending account.
	unknown := newHarness()
	unknown.email.failNext(1, configurationFailure())
	answerForUnknown := unknown.svc.Register(context.Background(), registerInput(email))

	// The address exists and is still pending, so the request takes the branch
	// that used to answer the ordinary accepted response without sending.
	existing := newHarness()
	if err := existing.svc.Register(context.Background(), registerInput(email)); err != nil {
		t.Fatalf("precondition: the address must be registrable: %v", err)
	}
	existing.email.failNext(1, configurationFailure())
	answerForExisting := existing.svc.Register(context.Background(), registerInput(email))

	if answerForUnknown != domainerr.ErrVerificationDeliveryFailed {
		t.Fatalf("an unknown address whose message cannot be delivered must answer the delivery failure, got %v", answerForUnknown)
	}
	if answerForExisting != answerForUnknown {
		t.Fatalf("both branches must answer the identical failure, so status, code and message cannot differ: unknown gave %v, pending gave %v",
			answerForUnknown, answerForExisting)
	}
}

// TestTheAlreadyRegisteredBranchAttemptsTheSameDelivery is the other half of
// FR-008a: the branches answer identically only because the second one now
// attempts the delivery the first one performs. A silent return would make the
// two answers differ again.
func TestTheAlreadyRegisteredBranchAttemptsTheSameDelivery(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	const email = "attempt@example.com"

	if err := h.svc.Register(ctx, registerInput(email)); err != nil {
		t.Fatalf("first: %v", err)
	}
	before := h.email.attempts()

	if err := h.svc.Register(ctx, registerInput(email)); err != nil {
		t.Fatalf("a delivered duplicate must answer the ordinary accepted response, got %v", err)
	}

	if h.email.attempts() != before+1 {
		t.Fatalf("expected exactly one delivery attempt on the duplicate, got %d", h.email.attempts()-before)
	}
	if h.email.count() != 2 {
		t.Fatalf("expected the message to reach the address, got %d delivered", h.email.count())
	}
}

// TestADuplicateRegistrationThatDeliversAnswersTheOrdinaryAcceptedResponse is
// FR-014 for the branch that now sends: while delivery works the answer must stay
// the one the endpoint has always given, and no failure trace may appear.
func TestADuplicateRegistrationThatDeliversAnswersTheOrdinaryAcceptedResponse(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	const email = "silent@example.com"

	if err := h.svc.Register(ctx, registerInput(email)); err != nil {
		t.Fatalf("first: %v", err)
	}
	if err := h.svc.Register(ctx, registerInput(email)); err != nil {
		t.Fatalf("duplicate: %v", err)
	}

	if h.audit.has(constant.AuditRegisterDeliveryFailed) {
		t.Fatalf("a delivered message must leave no failure trace, got %s", h.audit.traces())
	}
	if h.otp.disarmCount() != 0 {
		t.Fatalf("a delivered message must leave the cooldown armed, got %d disarms", h.otp.disarmCount())
	}
}

// TestAVerifiedAddressIsAnsweredTheOrdinaryAcceptedResponseAndIsNotSentACode
// keeps the boundary D4a draws: an address that is already confirmed needs no
// code, so sending one would change behaviour nobody asked for.
func TestAVerifiedAddressIsAnsweredTheOrdinaryAcceptedResponseAndIsNotSentACode(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	const email = "verified@example.com"

	if err := h.svc.Register(ctx, registerInput(email)); err != nil {
		t.Fatalf("first: %v", err)
	}
	if err := h.svc.VerifyEmail(ctx, appdto.VerifyEmailInput{Email: email, OTP: lastField(h.email.lastBody)}); err != nil {
		t.Fatalf("VerifyEmail: %v", err)
	}
	before := h.email.attempts()

	if err := h.svc.Register(ctx, registerInput(email)); err != nil {
		t.Fatalf("a verified address must answer the ordinary accepted response, got %v", err)
	}

	if h.email.attempts() != before {
		t.Fatalf("a verified account must not be sent a code, got %d send(s)", h.email.attempts()-before)
	}
	if h.audit.has(constant.AuditRegisterDeliveryFailed) {
		t.Fatalf("no delivery was attempted, so no failure trace may exist, got %s", h.audit.traces())
	}
}

// TestADuplicateRegistrationChangesNothingAboutTheStoredAccount covers FR-008 on
// the branch that now looks the account up: no second account, and none of the
// stored fields moved - not the identifier, not the password hash, not the state.
func TestADuplicateRegistrationChangesNothingAboutTheStoredAccount(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	const email = "single@example.com"

	if err := h.svc.Register(ctx, registerInput(email)); err != nil {
		t.Fatalf("first: %v", err)
	}
	before, err := h.users.ByEmail(ctx, email)
	if err != nil {
		t.Fatalf("precondition: %v", err)
	}

	if err := h.svc.Register(ctx, appdto.RegisterInput{Email: email, Password: "Another!Pass3"}); err != nil {
		t.Fatalf("duplicate with a different password must be accepted, got %v", err)
	}

	after, err := h.users.ByEmail(ctx, email)
	if err != nil {
		t.Fatalf("load after: %v", err)
	}
	if len(h.users.byEmail) != 1 {
		t.Fatalf("expected exactly one account, got %d", len(h.users.byEmail))
	}
	if after.ID != before.ID {
		t.Fatalf("expected the same account, got %s then %s", before.ID, after.ID)
	}
	if after.PasswordHash != before.PasswordHash {
		t.Fatal("a duplicate registration must not replace the stored password")
	}
	if after.Status != before.Status {
		t.Fatalf("a duplicate registration must not move the account state: %s then %s", before.Status, after.Status)
	}
}

// TestTheDuplicateBranchDisarmsTheCooldownWhenItsDeliveryFails covers FR-024 on
// the branch that now sends: the promise the 503 makes - request a new code -
// must hold whichever branch answered it.
func TestTheDuplicateBranchDisarmsTheCooldownWhenItsDeliveryFails(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	const email = "disarm@example.com"

	if err := h.svc.Register(ctx, registerInput(email)); err != nil {
		t.Fatalf("precondition: %v", err)
	}
	h.email.failNext(1, configurationFailure())
	if err := h.svc.Register(ctx, registerInput(email)); err == nil {
		t.Fatal("expected the failed delivery to surface")
	}
	if h.otp.disarmCount() != 1 {
		t.Fatalf("expected one disarm, got %d", h.otp.disarmCount())
	}
	if err := h.otp.CanResend(ctx, email); err != nil {
		t.Fatalf("a send that did not happen must not consume the cooldown: %v", err)
	}

	// The customer takes the step the answer names, and finishes on their own.
	if err := h.svc.ResendVerification(ctx, appdto.EmailInput{Email: email}); err != nil {
		t.Fatalf("a new code must be requestable right after a failed delivery: %v", err)
	}
	if err := h.svc.VerifyEmail(ctx, appdto.VerifyEmailInput{Email: email, OTP: lastField(h.email.lastBody)}); err != nil {
		t.Fatalf("the customer must be able to complete registration: %v", err)
	}
}

// TestTheDuplicateBranchTracesNameTheAccountAndCarryNothingElse covers FR-010,
// FR-011 and FR-012 on the branch that now reports a failure. The entry must
// identify the account the request concerned and must say, on the operator-facing
// line, that this request created nothing - so a failed duplicate is never read
// as a failed creation.
func TestTheDuplicateBranchTracesNameTheAccountAndCarryNothingElse(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	const (
		email      = "trace@example.com"
		credential = "smtp-credential-value-do-not-log"
	)
	if err := h.svc.Register(ctx, registerInput(email)); err != nil {
		t.Fatalf("precondition: %v", err)
	}
	account, err := h.users.ByEmail(ctx, email)
	if err != nil {
		t.Fatalf("precondition: %v", err)
	}

	var issued string
	h.email.failAlwaysWith(func(body string) error {
		issued = lastField(body)
		return fmt.Errorf("%w: unauthorized IP address 203.0.113.7, code %s, credential %s",
			domainerr.ErrDeliveryConfiguration, issued, credential)
	})

	if err := h.svc.Register(ctx, registerInput(email)); err != domainerr.ErrVerificationDeliveryFailed {
		t.Fatalf("expected the delivery failure to surface, got %v", err)
	}

	event, ok := h.audit.find(constant.AuditRegisterDeliveryFailed)
	if !ok {
		t.Fatalf("expected a %s entry, got %s", constant.AuditRegisterDeliveryFailed, h.audit.traces())
	}
	if event.outcome != constant.OutcomeFailure {
		t.Fatalf("expected outcome %s, got %s", constant.OutcomeFailure, event.outcome)
	}
	if event.targetID != account.ID.String() {
		t.Fatalf("expected the entry to identify account %s, got %q", account.ID, event.targetID)
	}
	// The audit metadata carries the classification and nothing else, as the
	// feature's data model requires.
	if len(event.metadata) != 1 || event.metadata["classification"] != constant.DeliveryConfiguration {
		t.Fatalf("expected the classification alone in the metadata, got %v", event.metadata)
	}

	// The operator-facing line names the account and says this request created
	// nothing, so a failed duplicate is never read as a failed creation.
	logs := h.logs.String()
	if !strings.Contains(logs, constant.DeliveryConfiguration) || !strings.Contains(logs, account.ID.String()) {
		t.Fatalf("expected one classified line naming the account, got %q", logs)
	}
	if !strings.Contains(logs, `"accountCreated":false`) {
		t.Fatalf("the operator must learn that this request created no account, got %q", logs)
	}
	traces := h.audit.traces() + "\n" + logs
	for _, secret := range []string{issued, credential, registerPassword, email, "203.0.113.7", "unauthorized IP address"} {
		if strings.Contains(traces, secret) {
			t.Fatalf("a trace leaked %q: %s", secret, traces)
		}
	}
}
