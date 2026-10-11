package migrations

import (
	"strings"
	"testing"
)

// paymentMigration introduces the two payment tables of module 08.
const paymentMigration = "00012_payment.sql"

// TestPaymentMigrationCreatesAndDropsEveryConstraint guards the storage-level
// invariants of the payment (FR-004, FR-009, FR-011, FR-012, FR-018, FR-022): every
// constraint and index must be created in the Up section and dropped in the Down
// section, so a rollback cannot leave one behind and the migration stays
// reversible.
func TestPaymentMigrationCreatesAndDropsEveryConstraint(t *testing.T) {
	upSection, downSection := readMigration(t, paymentMigration)

	cases := []struct {
		name     string
		upNeedle string
		down     string
	}{
		{
			name:     "four-state check",
			upNeedle: "ADD CONSTRAINT payments_status_ck CHECK",
			down:     "DROP CONSTRAINT payments_status_ck",
		},
		{
			name:     "currency check",
			upNeedle: "ADD CONSTRAINT payments_currency_ck CHECK",
			down:     "DROP CONSTRAINT payments_currency_ck",
		},
		{
			name:     "positive amount check",
			upNeedle: "ADD CONSTRAINT payments_amount_ck CHECK",
			down:     "DROP CONSTRAINT payments_amount_ck",
		},
		{
			name:     "positive order version check",
			upNeedle: "ADD CONSTRAINT payments_order_version_ck CHECK",
			down:     "DROP CONSTRAINT payments_order_version_ck",
		},
		{
			name:     "unmatched iff reason check",
			upNeedle: "ADD CONSTRAINT payments_unmatched_ck CHECK",
			down:     "DROP CONSTRAINT payments_unmatched_ck",
		},
		{
			name:     "unique order code",
			upNeedle: "CREATE UNIQUE INDEX payments_order_code_key",
			down:     "DROP INDEX payments_order_code_key",
		},
		{
			name:     "partial unique provider reference",
			upNeedle: "CREATE UNIQUE INDEX payments_provider_reference_key",
			down:     "DROP INDEX payments_provider_reference_key",
		},
		{
			name:     "unique live payment per order",
			upNeedle: "CREATE UNIQUE INDEX payments_live_order_key",
			down:     "DROP INDEX IF EXISTS payments_live_order_key",
		},
		{
			name:     "order read index",
			upNeedle: "CREATE INDEX payments_order_idx",
			down:     "DROP INDEX payments_order_idx",
		},
		{
			name:     "status filter index",
			upNeedle: "CREATE INDEX payments_status_idx",
			down:     "DROP INDEX payments_status_idx",
		},
		{
			name:     "cascading events foreign key",
			upNeedle: "ADD CONSTRAINT payment_events_payment_fk FOREIGN KEY",
			down:     "DROP CONSTRAINT payment_events_payment_fk",
		},
		{
			name:     "payment event action check",
			upNeedle: "ADD CONSTRAINT payment_events_action_ck CHECK",
			down:     "DROP CONSTRAINT payment_events_action_ck",
		},
		{
			name:     "payment event history index",
			upNeedle: "CREATE INDEX payment_events_payment_idx",
			down:     "DROP INDEX payment_events_payment_idx",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !strings.Contains(upSection, tc.upNeedle) {
				t.Errorf("%s: Up section does not create %q", paymentMigration, tc.upNeedle)
			}
			if !strings.Contains(downSection, tc.down) {
				t.Errorf("%s: Down section does not drop %q", paymentMigration, tc.down)
			}
		})
	}

	for _, table := range []string{"payments", "payment_events"} {
		if !strings.Contains(downSection, "DROP TABLE "+table) {
			t.Errorf("%s: Down section does not drop the %s table", paymentMigration, table)
		}
	}
}

// TestPaymentMigrationListsTheFourStates checks the stored status cannot be a
// value the domain does not know (FR-011, research D6).
func TestPaymentMigrationListsTheFourStates(t *testing.T) {
	up, _ := readMigration(t, paymentMigration)

	index := strings.Index(up, "payments_status_ck CHECK")
	if index < 0 {
		t.Fatalf("%s: Up section does not declare payments_status_ck", paymentMigration)
	}
	window := up[index:]
	if end := strings.Index(window, ";"); end >= 0 {
		window = window[:end]
	}
	for _, status := range []string{"'PENDING'", "'PAID'", "'CANCELLED'", "'UNMATCHED'"} {
		if !strings.Contains(window, status) {
			t.Errorf("%s: the status check does not list the state %s", paymentMigration, status)
		}
	}
}

// TestPaymentMigrationRefusesBadAmountsAndCurrencies pins the check constraints
// FR-004 and FR-012 rest on: a non-positive amount, a currency that is not three
// uppercase letters and a non-positive order version are refused by the database,
// not only by the application.
func TestPaymentMigrationRefusesBadAmountsAndCurrencies(t *testing.T) {
	up, _ := readMigration(t, paymentMigration)

	for _, check := range []string{
		"CHECK (amount > 0)",
		"CHECK (order_version >= 1)",
		`CHECK (currency ~ '^[A-Z]{3}$')`,
	} {
		if !strings.Contains(up, check) {
			t.Errorf("%s: Up section does not carry the check %q", paymentMigration, check)
		}
	}
}

// TestPaymentMigrationTiesUnmatchedToAReason pins FR-012 and FR-013: a payment is
// UNMATCHED iff it carries a reason, so an anomaly queue entry always explains
// itself and no other state carries a reason.
func TestPaymentMigrationTiesUnmatchedToAReason(t *testing.T) {
	up, _ := readMigration(t, paymentMigration)

	const check = "CHECK ((status = 'UNMATCHED') = (unmatched_reason IS NOT NULL))"
	if !strings.Contains(up, check) {
		t.Fatalf("%s: Up section does not tie UNMATCHED to a reason, want %q", paymentMigration, check)
	}
}

// TestPaymentMigrationUniquesTheOrderCode pins research D4: a collision must be
// retried by the module rather than stored twice.
func TestPaymentMigrationUniquesTheOrderCode(t *testing.T) {
	up, _ := readMigration(t, paymentMigration)

	if !strings.Contains(up, "CREATE UNIQUE INDEX payments_order_code_key ON payments (order_code)") {
		t.Errorf("%s: Up section must unique the order code", paymentMigration)
	}
}

// TestPaymentMigrationPartiallyUniquesTheProviderReference pins research D7: the
// provider reference is the idempotency key, and the index is partial so several
// rows may hold no reference until one confirms.
func TestPaymentMigrationPartiallyUniquesTheProviderReference(t *testing.T) {
	up, _ := readMigration(t, paymentMigration)

	index := strings.Index(up, "CREATE UNIQUE INDEX payments_provider_reference_key")
	if index < 0 {
		t.Fatalf("%s: Up section does not declare the provider reference key", paymentMigration)
	}
	window := up[index:]
	if end := strings.Index(window, ";"); end >= 0 {
		window = window[:end+1]
	}
	if !strings.Contains(window, "ON payments (provider_reference)") {
		t.Errorf("%s: the provider reference key must be on payments (provider_reference)", paymentMigration)
	}
	if !strings.Contains(window, "WHERE provider_reference IS NOT NULL") {
		t.Errorf("%s: the provider reference key must be partial (WHERE provider_reference IS NOT NULL)", paymentMigration)
	}
}

// TestPaymentMigrationPartiallyUniquesTheLivePaymentPerOrder pins FR-005 and
// Constitution II: at most one live payment per order, enforced by the storage.
// Two PENDING rows for one order are refused; a PENDING row and a terminal row
// for the same order are allowed, because the unique index is partial on the live
// state.
func TestPaymentMigrationPartiallyUniquesTheLivePaymentPerOrder(t *testing.T) {
	up, _ := readMigration(t, paymentMigration)

	index := strings.Index(up, "CREATE UNIQUE INDEX payments_live_order_key")
	if index < 0 {
		t.Fatalf("%s: Up section does not declare the live-payment key", paymentMigration)
	}
	window := up[index:]
	if end := strings.Index(window, ";"); end >= 0 {
		window = window[:end+1]
	}
	if !strings.Contains(window, "ON payments (order_id)") {
		t.Errorf("%s: the live-payment key must be on payments (order_id)", paymentMigration)
	}
	if !strings.Contains(window, "WHERE status = 'PENDING'") {
		t.Errorf("%s: the live-payment key must be partial (WHERE status = 'PENDING')", paymentMigration)
	}
}

// TestPaymentEventsCascadeFromPayments pins FR-022: a removed payment takes its
// events with it, through the cascading foreign key.
func TestPaymentEventsCascadeFromPayments(t *testing.T) {
	up, _ := readMigration(t, paymentMigration)

	index := strings.Index(up, "payment_events_payment_fk FOREIGN KEY")
	if index < 0 {
		t.Fatalf("%s: Up section does not declare payment_events_payment_fk", paymentMigration)
	}
	window := up[index:]
	if end := strings.Index(window, ";"); end >= 0 {
		window = window[:end+1]
	}
	if !strings.Contains(window, "REFERENCES payments(id) ON DELETE CASCADE") {
		t.Errorf("%s: payment_events_payment_fk must reference payments(id) with ON DELETE CASCADE", paymentMigration)
	}
}

// TestPaymentEventsActionCheckListsTheFourActions pins FR-022: a written event
// always carries a canonical action.
func TestPaymentEventsActionCheckListsTheFourActions(t *testing.T) {
	up, _ := readMigration(t, paymentMigration)

	index := strings.Index(up, "payment_events_action_ck CHECK")
	if index < 0 {
		t.Fatalf("%s: Up section does not declare payment_events_action_ck", paymentMigration)
	}
	window := up[index:]
	if end := strings.Index(window, ";"); end >= 0 {
		window = window[:end]
	}
	for _, action := range []string{"'PAYMENT_CREATED'", "'PAYMENT_APPLIED'", "'PAYMENT_CANCELLED'", "'PAYMENT_UNMATCHED'"} {
		if !strings.Contains(window, action) {
			t.Errorf("%s: the action check does not list %s", paymentMigration, action)
		}
	}
}

// TestPaymentMigrationKeepsTheOrderAndOwnerReferencesLoose pins research D5: the
// payment and the order live in different modules, so neither order_id nor user_id
// carries a foreign key. The assertion is on constraint declarations, not on the
// comment prose, so a later edit that adds a key is caught.
func TestPaymentMigrationKeepsTheOrderAndOwnerReferencesLoose(t *testing.T) {
	up, _ := readMigration(t, paymentMigration)

	for _, forbidden := range []string{
		"FOREIGN KEY (order_id)",
		"REFERENCES orders",
		"FOREIGN KEY (user_id)",
		"REFERENCES users",
	} {
		if strings.Contains(up, forbidden) {
			t.Errorf("%s: the order and owner references must be loose, found %q", paymentMigration, forbidden)
		}
	}
}
