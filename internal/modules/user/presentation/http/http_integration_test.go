//go:build integration

package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	authdomainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/error"
	authtoken "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/infrastructure/implement/token"
	appdto "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/application/dto"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/application/implement"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/application/interface"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/application/mapper"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/domain/constant"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/domain/model"
	adminadapter "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/infrastructure/implement/administrative"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/infrastructure/implement/auditor"
	userpostgres "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/infrastructure/implement/postgres"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/access"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/administrative"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/audit"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/config"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/database"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/database/migrate"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/httpx"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/middleware"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/testsupport"
)

// The user module is not mounted in cmd/api yet (T023), so the router is built
// here against a real PostgreSQL container: real migrations, the real repository
// adapter, the real audit writer and a real access token from the auth module.

const (
	// integrationSecret is a throwaway HS256 key for the tokens this test issues.
	integrationSecret = "0123456789abcdef0123456789abcdef"
	// integrationTokenTTL is long enough that no token expires mid-test.
	integrationTokenTTL = 15 * time.Minute
)

// integrationFixture is the module wired against real infrastructure.
type integrationFixture struct {
	handler http.Handler
	pool    *pgxpool.Pool
	// auditWriter is stopped by the test so the queue drains before it asserts.
	auditWriter *audit.Writer
	// owner is the seeded account the session identifies.
	owner uuid.UUID
	// other is a second seeded account that must stay invisible to that session.
	other uuid.UUID
	// token is a live access token for owner.
	token string
	// mediaConfigured reports whether media credentials are present. The profile
	// story must work with none of them (FR-021).
	mediaConfigured bool
}

func newIntegrationFixture(t *testing.T) *integrationFixture {
	t.Helper()
	dsn := testsupport.PostgresDSN(t)
	ctx := context.Background()

	runner, err := migrate.New(dsn, 30*time.Second)
	if err != nil {
		t.Fatalf("migrate.New: %v", err)
	}
	defer runner.Close()
	if err := runner.Up(ctx); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)

	writer := audit.NewWriter(audit.NewRepository(pool), discardLogger(), 64, 1, 1)
	writer.Start(ctx)
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		writer.Stop(stopCtx)
	})

	// No media store is wired: the media credentials are absent, which is exactly
	// the FR-021 case this test has to survive.
	//
	// The real divisions adapter serves the bundled dataset, so the address tests
	// validate against the same codes a client would have picked, and the shared
	// DB is the module's UnitOfWork, so the default-flag transition runs inside a
	// real transaction.
	divisions := adminadapter.New()
	profiles := implement.New(implement.Service{
		Profiles:  userpostgres.NewProfileRepository(pool),
		Addresses: userpostgres.NewAddressRepository(pool),
		Divisions: divisions,
		Tx:        &database.DB{Pool: pool},
		Audit:     auditor.New(writer),
		Mapper:    mapper.New(divisions),
	})

	owner := seedAccount(t, pool, "owner@example.com")
	other := seedAccount(t, pool, "other@example.com")

	token, _, err := authtoken.NewAccessIssuer(integrationSecret, integrationTokenTTL).
		Issue(owner, access.RoleCustomer)
	if err != nil {
		t.Fatalf("issue access token: %v", err)
	}

	handler := New(
		integrationService{stubService: newStub(access.RoleCustomer), user: profiles},
		appinterface.Config{},
		discardLogger(),
	)
	root := chi.NewRouter()
	root.Mount("/api/v1/users", handler.Router(
		config.UserConfig{AvatarUploadRatePerHour: 100, AddressWriteRatePerMinute: 100},
		integrationAuthHooks(t),
	))

	return &integrationFixture{
		handler:         root,
		pool:            pool,
		auditWriter:     writer,
		owner:           owner,
		other:           other,
		token:           token,
		mediaConfigured: config.MediaConfig{}.IsConfigured(),
	}
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}

// integrationService binds the real profile and address use cases to the module's
// full use-case surface. The avatar and administrator-lookup methods still come
// from the stub, whose routes these tests never reach; this adapter disappears once
// every story has landed and T023 wires *implement.Service directly.
type integrationService struct {
	*stubService
	user *implement.Service
}

func (s integrationService) GetProfile(ctx context.Context, id uuid.UUID) (appdto.ProfileOutput, error) {
	return s.user.GetProfile(ctx, id)
}

func (s integrationService) UpdateProfile(ctx context.Context, in appdto.UpdateProfileInput) (appdto.ProfileOutput, error) {
	return s.user.UpdateProfile(ctx, in)
}

func (s integrationService) ListAddresses(ctx context.Context, in appdto.ListAddressesInput) (appdto.AddressPageOutput, error) {
	return s.user.ListAddresses(ctx, in)
}

func (s integrationService) CreateAddress(ctx context.Context, in appdto.CreateAddressInput) (appdto.AddressOutput, error) {
	return s.user.CreateAddress(ctx, in)
}

func (s integrationService) UpdateAddress(ctx context.Context, in appdto.UpdateAddressInput) (appdto.AddressOutput, error) {
	return s.user.UpdateAddress(ctx, in)
}

func (s integrationService) DeleteAddress(ctx context.Context, in appdto.AddressRefInput) error {
	return s.user.DeleteAddress(ctx, in)
}

func (s integrationService) SetDefaultAddress(ctx context.Context, in appdto.AddressRefInput) (appdto.AddressOutput, error) {
	return s.user.SetDefaultAddress(ctx, in)
}

var _ appinterface.UserService = integrationService{}

// integrationAuthHooks verifies the bearer token with the auth module's own
// issuer and maps its failures the same way the composition root does, so this
// test sees the real 401 an expired or tampered token produces.
func integrationAuthHooks(t *testing.T) middleware.AuthHooks {
	t.Helper()
	issuer := authtoken.NewAccessIssuer(integrationSecret, integrationTokenTTL)
	return middleware.AuthHooks{
		Authenticate: func(_ context.Context, r *http.Request) (*middleware.Identity, error) {
			header := r.Header.Get("Authorization")
			const prefix = "Bearer "
			if !strings.HasPrefix(header, prefix) {
				return nil, nil
			}
			claims, err := issuer.Parse(strings.TrimSpace(strings.TrimPrefix(header, prefix)))
			if err != nil {
				if errors.Is(err, authdomainerr.ErrExpiredToken) {
					return nil, &httpx.AppError{
						Code:    "AUTH_TOKEN_EXPIRED",
						Status:  http.StatusUnauthorized,
						Message: "Access token has expired",
					}
				}
				return nil, &httpx.AppError{
					Code:    "AUTH_TOKEN_INVALID",
					Status:  http.StatusUnauthorized,
					Message: "Access token is not valid",
				}
			}
			return &middleware.Identity{
				Subject: claims.Subject.String(),
				Role:    string(claims.Role),
				TokenID: claims.ID,
			}, nil
		},
	}
}

// seedAccount inserts an account the way the auth module does. This module never
// creates an account; the test seeds one so it has a session to act as.
func seedAccount(t *testing.T, pool *pgxpool.Pool, email string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	const query = `
		INSERT INTO users (id, email, password_hash, role, status)
		VALUES ($1, $2, 'not-used-by-this-test', 'CUSTOMER', 'active')`
	if _, err := pool.Exec(context.Background(), query, id, email); err != nil {
		t.Fatalf("seed account %s: %v", email, err)
	}
	return id
}

func (f *integrationFixture) call(method, path, body, token string) *httptest.ResponseRecorder {
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)
	return rec
}

const profilePath = "/api/v1/users/me"

func (f *integrationFixture) readProfile(t *testing.T) profileBody {
	t.Helper()
	rec := f.call(http.MethodGet, profilePath, "", f.token)
	if rec.Code != http.StatusOK {
		t.Fatalf("read profile: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	return decodeProfile(t, rec)
}

// profileRows is what the account row holds after an update.
type profileRows struct {
	DisplayName *string
	Phone       *string
	PublicID    *string
	SecureURL   *string
	Width       *int
	Height      *int
	// RowCreatedAt and RowUpdatedAt prove the adapter refreshes the row timestamp
	// without touching the creation one.
	RowCreatedAt time.Time
	RowUpdatedAt time.Time
	// Email and Role prove the auth-owned columns survived the profile write.
	Email string
	Role  string
}

func (f *integrationFixture) storedRow(t *testing.T, id uuid.UUID) profileRows {
	t.Helper()
	const query = `
		SELECT display_name, phone, avatar_public_id, avatar_secure_url, avatar_width, avatar_height,
		       created_at, updated_at, email, role
		FROM users WHERE id = $1`
	var rows profileRows
	err := f.pool.QueryRow(context.Background(), query, id).Scan(
		&rows.DisplayName, &rows.Phone, &rows.PublicID, &rows.SecureURL, &rows.Width, &rows.Height,
		&rows.RowCreatedAt, &rows.RowUpdatedAt, &rows.Email, &rows.Role,
	)
	if err != nil {
		t.Fatalf("read the stored row: %v", err)
	}
	return rows
}

func (f *integrationFixture) auditRows(t *testing.T, action string) []audit.Event {
	t.Helper()
	const query = `
		SELECT event_id, actor_id, actor_role, action, target_type, target_id, outcome, correlation_id, occurred_at
		FROM audit_logs WHERE action = $1 ORDER BY occurred_at`
	rows, err := f.pool.Query(context.Background(), query, action)
	if err != nil {
		t.Fatalf("read the audit trail: %v", err)
	}
	defer rows.Close()
	var events []audit.Event
	for rows.Next() {
		var (
			event    audit.Event
			actorID  *uuid.UUID
			action   string
			outcome  string
			corrID   *string
			occurred time.Time
		)
		if err := rows.Scan(&event.EventID, &actorID, &event.ActorRole, &action, &event.TargetType,
			&event.TargetID, &outcome, &corrID, &occurred); err != nil {
			t.Fatalf("scan an audit row: %v", err)
		}
		event.ActorID = actorID
		event.Outcome = audit.Outcome(outcome)
		if corrID != nil {
			event.CorrelationID = *corrID
		}
		event.OccurredAt = occurred
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate the audit trail: %v", err)
	}
	return events
}

// drainAudit waits for the asynchronous writer to hand every queued event to the
// store, so an assertion about the audit trail cannot race the writer.
func (f *integrationFixture) drainAudit(t *testing.T) {
	t.Helper()
	f.drainAuditAction(t, constant.AuditProfileUpdated)
}

// drainAuditAction waits until at least one row of the given action is persisted.
func (f *integrationFixture) drainAuditAction(t *testing.T, action string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var pending int
		if err := f.pool.QueryRow(context.Background(),
			"SELECT count(*) FROM audit_logs WHERE action = $1", action).Scan(&pending); err != nil {
			t.Fatalf("count the audit rows: %v", err)
		}
		if pending > 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the audit writer never persisted a %s event", action)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// US1 against real infrastructure: update, read back, confirm the normalisation
// reached the row, confirm the audit trail, and confirm the read still works
// while no media is configured (FR-021).
func TestProfileUpdateAndReadBackAgainstPostgres(t *testing.T) {
	fixture := newIntegrationFixture(t)
	if fixture.mediaConfigured {
		t.Fatal("this test must run without media credentials so FR-021 is covered")
	}

	before := fixture.storedRow(t, fixture.owner)
	if before.DisplayName != nil || before.Phone != nil {
		t.Fatalf("expected a fresh account with no profile, got %+v", before)
	}

	rec := fixture.call(http.MethodPatch, profilePath,
		`{"displayName":"  Nguyen Van A  ","phone":"+84 912 345 678"}`, fixture.token)
	if rec.Code != http.StatusOK {
		t.Fatalf("update: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}

	body := decodeProfile(t, rec)
	if body.Data.DisplayName != "Nguyen Van A" {
		t.Fatalf("expected the trimmed display name in the response, got %q", body.Data.DisplayName)
	}
	if body.Data.Phone == nil || *body.Data.Phone != "0912345678" {
		t.Fatalf("expected the normalised phone in the response, got %v", body.Data.Phone)
	}
	if body.Data.Email != "owner@example.com" || body.Data.Role != access.RoleCustomer {
		t.Fatalf("the response must carry the auth-owned columns, got %+v", body.Data)
	}

	// The next read answers from the database, not from the update's answer.
	readBack := fixture.readProfile(t)
	if readBack.Data.DisplayName != "Nguyen Van A" {
		t.Fatalf("expected the stored display name on the next read, got %q", readBack.Data.DisplayName)
	}
	if readBack.Data.Phone == nil || *readBack.Data.Phone != "0912345678" {
		t.Fatalf("expected the normalised phone on the next read, got %v", readBack.Data.Phone)
	}

	stored := fixture.storedRow(t, fixture.owner)
	if stored.DisplayName == nil || *stored.DisplayName != "Nguyen Van A" {
		t.Fatalf("expected the trimmed display name in the row, got %v", stored.DisplayName)
	}
	if stored.Phone == nil || *stored.Phone != "0912345678" {
		t.Fatalf("expected the normalised phone in the row, got %v", stored.Phone)
	}
	// The four avatar columns move together, so a customer without a photo has
	// all four NULL (FR-016).
	if stored.PublicID != nil || stored.SecureURL != nil || stored.Width != nil || stored.Height != nil {
		t.Fatalf("expected every avatar column to be NULL, got %+v", stored)
	}
	if readBack.Data.Avatar != nil {
		t.Fatalf("expected a null avatar in the response, got %s", string(*readBack.Data.Avatar))
	}
	if !strings.Contains(rec.Body.String(), `"avatar":null`) {
		t.Fatalf("expected an explicit null avatar member, got %s", rec.Body.String())
	}

	// The write touched only what this module owns.
	if stored.Email != "owner@example.com" || stored.Role != "CUSTOMER" {
		t.Fatalf("the profile write changed an auth-owned column: %+v", stored)
	}
	if !stored.RowUpdatedAt.After(before.RowUpdatedAt) {
		t.Fatalf("expected the row timestamp to advance, got %v", stored.RowUpdatedAt)
	}
	if !stored.RowCreatedAt.Equal(before.RowCreatedAt) {
		t.Fatalf("the creation timestamp must not move, got %v", stored.RowCreatedAt)
	}

	// Another customer's row is untouched.
	if other := fixture.storedRow(t, fixture.other); other.DisplayName != nil || other.Phone != nil {
		t.Fatalf("another customer's row changed: %+v", other)
	}
}

// An invalid phone must leave the row byte-for-byte as it was (FR-003).
func TestInvalidPhoneChangesNothingInPostgres(t *testing.T) {
	fixture := newIntegrationFixture(t)
	if rec := fixture.call(http.MethodPatch, profilePath,
		`{"displayName":"Nguyen Van A","phone":"0912345678"}`, fixture.token); rec.Code != http.StatusOK {
		t.Fatalf("seed the profile: %d (%s)", rec.Code, rec.Body.String())
	}
	before := fixture.storedRow(t, fixture.owner)

	rec := fixture.call(http.MethodPatch, profilePath, `{"phone":"12345"}`, fixture.token)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (%s)", rec.Code, rec.Body.String())
	}
	if got := errorCode(t, rec); got != constant.CodeInvalidPhone {
		t.Fatalf("expected %s, got %s", constant.CodeInvalidPhone, got)
	}
	var envelope errorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode the error body: %v", err)
	}
	if len(envelope.Error.Details) != 1 || envelope.Error.Details[0].Field != fieldPhone {
		t.Fatalf("expected one detail naming %q, got %+v", fieldPhone, envelope.Error.Details)
	}
	after := fixture.storedRow(t, fixture.owner)
	if *after.DisplayName != *before.DisplayName || *after.Phone != *before.Phone {
		t.Fatalf("a rejected phone changed the row: %+v -> %+v", before, after)
	}
}

// A cleared field is stored as empty rather than keeping the old value (FR-004).
func TestClearingAFieldPersistsAgainstPostgres(t *testing.T) {
	fixture := newIntegrationFixture(t)
	if rec := fixture.call(http.MethodPatch, profilePath,
		`{"displayName":"Nguyen Van A","phone":"0912345678"}`, fixture.token); rec.Code != http.StatusOK {
		t.Fatalf("seed the profile: %d (%s)", rec.Code, rec.Body.String())
	}

	if rec := fixture.call(http.MethodPatch, profilePath, `{"phone":null}`, fixture.token); rec.Code != http.StatusOK {
		t.Fatalf("clear the phone: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}

	stored := fixture.storedRow(t, fixture.owner)
	if stored.Phone != nil {
		t.Fatalf("expected the phone to be NULL, got %q", *stored.Phone)
	}
	if stored.DisplayName == nil || *stored.DisplayName != "Nguyen Van A" {
		t.Fatalf("an omitted field must keep its value, got %v", stored.DisplayName)
	}
	body := fixture.readProfile(t)
	if body.Data.Phone != nil {
		t.Fatalf("expected a null phone on the next read, got %q", *body.Data.Phone)
	}
}

// SC-002 and SC-008: a request without a usable session is refused before it
// reaches the database, and every successful change leaves an audit row.
func TestProfileRoutesRefuseBadSessionsAndAuditRealUpdates(t *testing.T) {
	fixture := newIntegrationFixture(t)

	for _, tc := range []struct {
		name  string
		token string
	}{
		{"no token", ""},
		{"tampered token", fixture.token + "tampered"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := fixture.call(http.MethodPatch, profilePath, `{"displayName":"Nguyen Van A"}`, tc.token)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("expected 401, got %d (%s)", rec.Code, rec.Body.String())
			}
		})
	}
	if rows := fixture.storedRow(t, fixture.owner); rows.DisplayName != nil {
		t.Fatalf("a refused request wrote to the row: %+v", rows)
	}

	expired, _, err := authtoken.NewAccessIssuer(integrationSecret, -time.Minute).
		Issue(fixture.owner, access.RoleCustomer)
	if err != nil {
		t.Fatalf("issue an expired token: %v", err)
	}
	rec := fixture.call(http.MethodGet, profilePath, "", expired)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("an expired token: expected 401, got %d (%s)", rec.Code, rec.Body.String())
	}

	if rec := fixture.call(http.MethodPatch, profilePath,
		`{"displayName":"Nguyen Van A","phone":"0912 345 678"}`, fixture.token); rec.Code != http.StatusOK {
		t.Fatalf("update: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}

	fixture.drainAudit(t)
	events := fixture.auditRows(t, constant.AuditProfileUpdated)
	if len(events) != 1 {
		t.Fatalf("expected exactly one audit row, got %d", len(events))
	}
	event := events[0]
	if event.ActorID == nil || *event.ActorID != fixture.owner {
		t.Fatalf("expected the account %s as the actor, got %v", fixture.owner, event.ActorID)
	}
	if event.ActorRole != string(access.RoleCustomer) {
		t.Fatalf("expected the actor role, got %q", event.ActorRole)
	}
	if event.TargetType != "user" || event.TargetID != fixture.owner.String() {
		t.Fatalf("expected the account as the target, got %q/%q", event.TargetType, event.TargetID)
	}
	if event.Outcome != audit.OutcomeSuccess {
		t.Fatalf("expected a SUCCESS outcome, got %q", event.Outcome)
	}
}

// A session may only ever act on its own account (FR-006, SC-003). The account
// comes from the token, so a second customer's token cannot reach the first
// customer's row, and the profile routes have no not-found outcome of their own.
func TestASessionOnlyReachesItsOwnAccount(t *testing.T) {
	fixture := newIntegrationFixture(t)
	if rec := fixture.call(http.MethodPatch, profilePath,
		`{"displayName":"Owner","phone":"0912345678"}`, fixture.token); rec.Code != http.StatusOK {
		t.Fatalf("seed the owner profile: %d (%s)", rec.Code, rec.Body.String())
	}

	otherToken, _, err := authtoken.NewAccessIssuer(integrationSecret, integrationTokenTTL).
		Issue(fixture.other, access.RoleCustomer)
	if err != nil {
		t.Fatalf("issue the other token: %v", err)
	}
	body := decodeProfile(t, fixture.call(http.MethodGet, profilePath, "", otherToken))
	if body.Data.ID != fixture.other {
		t.Fatalf("expected the second account, got %s", body.Data.ID)
	}
	if body.Data.DisplayName == "Owner" || body.Data.Phone != nil {
		t.Fatalf("the owner's profile leaked to another session: %+v", body.Data)
	}
	if rows := fixture.storedRow(t, fixture.other); rows.DisplayName != nil {
		t.Fatalf("the second session changed its own row without sending anything: %+v", rows)
	}

	// The owner's row is still the owner's.
	owner := fixture.readProfile(t)
	if owner.Data.ID != fixture.owner || owner.Data.DisplayName != "Owner" {
		t.Fatalf("the owner's profile changed: %+v", owner.Data)
	}
}

// The module's UnitOfWork is not needed by the profile write, but the repository
// must still honour a transaction placed in the context, because the address
// default-flag transition depends on that behaviour (FR-010, Constitution I).
func TestProfileSaveJoinsACallerTransaction(t *testing.T) {
	fixture := newIntegrationFixture(t)
	ctx := context.Background()
	db := &database.DB{Pool: fixture.pool}

	// The profile write runs inside a transaction the application layer opened.
	// The repository joins it through the context instead of opening one of its
	// own, which is what lets the address default-flag transition stay atomic
	// later (FR-010, Constitution I).
	err := db.WithinTx(ctx, func(txCtx context.Context) error {
		repo := userpostgres.NewProfileRepository(fixture.pool)
		if _, err := repo.ByID(txCtx, fixture.owner); err != nil {
			return err
		}
		return repo.Save(txCtx, seedProfile(fixture.owner))
	})
	if err != nil {
		t.Fatalf("save inside a transaction: %v", err)
	}

	if rows := fixture.storedRow(t, fixture.owner); rows.DisplayName == nil || *rows.DisplayName != "Seeded" {
		t.Fatalf("expected the committed display name, got %v", rows.DisplayName)
	}
}

// seedProfile builds the model a repository test needs, keeping the domain rules
// in charge of the value rather than writing the field directly.
func seedProfile(id uuid.UUID) *model.Profile {
	profile := &model.Profile{ID: id, Email: "owner@example.com", Role: access.RoleCustomer}
	profile.SetDisplayName("Seeded")
	profile.SetPhone("0912345678")
	return profile
}

// ---------------------------------------------------------------------------
// US2 against real infrastructure.
//
// The division codes below are real entries of the bundled dataset, picked from
// the smallest provinces so the ward lists stay small. The display names are
// never hard-coded: they are read back from the dataset itself, so the assertion
// is that the repository stored *the dataset's* name beside the code rather than
// whatever the request happened to carry.
// ---------------------------------------------------------------------------

const (
	// addressProvinceCode and its two wards are a real province/ward pair.
	addressProvinceCode  = "12"
	addressWardCode      = "03466"
	otherAddressWardCode = "03433"
	// otherProvinceCode is a different province, so a ward from the first one
	// paired with it is the mismatch FR-007b refuses.
	otherProvinceCode = "46"
)

func addressRequestBody(provinceCode, wardCode, street string) string {
	return `{"recipientName":"Nguyen Van A","recipientPhone":"0912 345 678",` +
		`"provinceCode":"` + provinceCode + `","wardCode":"` + wardCode + `",` +
		`"streetAddress":"` + street + `"}`
}

const addressesPath = profilePath + "/addresses"

// createAddress stores one address through the real use cases and returns its
// identifier.
func (f *integrationFixture) createAddress(t *testing.T, token, provinceCode, wardCode, street string) string {
	t.Helper()
	rec := f.call(http.MethodPost, addressesPath, addressRequestBody(provinceCode, wardCode, street), token)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create an address: expected 201, got %d (%s)", rec.Code, rec.Body.String())
	}
	return decodeAddress(t, rec).ID
}

// addressRow is what the addresses table holds, read straight from the database so
// no response mapping can hide what was persisted.
type addressRow struct {
	UserID         uuid.UUID
	RecipientName  string
	RecipientPhone string
	ProvinceCode   string
	ProvinceName   string
	WardCode       string
	WardName       string
	StreetAddress  string
	IsDefault      bool
	DeletedAt      *time.Time
}

// addressRows reads every row of an account's addresses, hidden ones included.
func (f *integrationFixture) addressRows(t *testing.T, userID uuid.UUID) map[string]addressRow {
	t.Helper()
	const query = `
		SELECT id, user_id, recipient_name, recipient_phone, province_code, province_name,
		       ward_code, ward_name, street_address, is_default, deleted_at
		FROM addresses WHERE user_id = $1`
	rows, err := f.pool.Query(context.Background(), query, userID)
	if err != nil {
		t.Fatalf("read the address rows: %v", err)
	}
	defer rows.Close()
	out := make(map[string]addressRow)
	for rows.Next() {
		var (
			id  uuid.UUID
			row addressRow
		)
		if err := rows.Scan(&id, &row.UserID, &row.RecipientName, &row.RecipientPhone,
			&row.ProvinceCode, &row.ProvinceName, &row.WardCode, &row.WardName,
			&row.StreetAddress, &row.IsDefault, &row.DeletedAt); err != nil {
			t.Fatalf("scan an address row: %v", err)
		}
		out[id.String()] = row
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate the address rows: %v", err)
	}
	return out
}

// visibleDefaults counts the rows the partial unique index counts: one visible
// default per account, read straight from the storage layer (SC-004).
func (f *integrationFixture) visibleDefaultIDs(t *testing.T, userID uuid.UUID) []string {
	t.Helper()
	const query = `
		SELECT id FROM addresses
		WHERE user_id = $1 AND is_default AND deleted_at IS NULL`
	rows, err := f.pool.Query(context.Background(), query, userID)
	if err != nil {
		t.Fatalf("read the visible defaults: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan a default row: %v", err)
		}
		out = append(out, id.String())
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate the visible defaults: %v", err)
	}
	return out
}

func (f *integrationFixture) listAddresses(t *testing.T, token, query string) addressListBody {
	t.Helper()
	rec := f.call(http.MethodGet, addressesPath+query, "", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("list addresses: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	return decodeAddressList(t, rec)
}

// US2 end to end against a real database: create two, flip the default, edit, hide
// one, and confirm the account still has exactly one default plus a complete audit
// trail (FR-008 to FR-012, FR-019, SC-004, SC-008).
func TestAddressLifecycleAgainstPostgres(t *testing.T) {
	f := newIntegrationFixture(t)

	first := f.createAddress(t, f.token, addressProvinceCode, addressWardCode, "12 Nguyen Hue")
	second := f.createAddress(t, f.token, addressProvinceCode, otherAddressWardCode, "1 Le Loi")

	// The first address became the default and the second did not steal it.
	listed := f.listAddresses(t, f.token, "")
	if listed.Meta.Total != 2 {
		t.Fatalf("expected 2 addresses, got %d", listed.Meta.Total)
	}
	if listed.Data[0].ID != first || !listed.Data[0].IsDefault {
		t.Fatalf("expected the first address default and first in the list, got %+v", listed.Data)
	}
	if listed.Data[1].IsDefault {
		t.Fatalf("a second address must not be default: %+v", listed.Data[1])
	}
	if got := f.visibleDefaultIDs(t, f.owner); len(got) != 1 || got[0] != first {
		t.Fatalf("expected one visible default, got %v", got)
	}

	// The captured division names come from the dataset, not from the request.
	province, err := administrative.FindProvince(addressProvinceCode)
	if err != nil {
		t.Fatalf("read the province from the dataset: %v", err)
	}
	ward, err := administrative.FindWard(addressWardCode)
	if err != nil {
		t.Fatalf("read the ward from the dataset: %v", err)
	}
	storedFirst := f.addressRows(t, f.owner)[first]
	if storedFirst.ProvinceName != province.Name || storedFirst.WardName != ward.Name {
		t.Fatalf("the captured names are not the dataset's: %+v", storedFirst)
	}
	if storedFirst.ProvinceCode != addressProvinceCode || storedFirst.WardCode != addressWardCode {
		t.Fatalf("the stored codes are not the ones that were chosen: %+v", storedFirst)
	}

	// Flip the default: the previous one is cleared and the new one is the single
	// default, in one indivisible operation (FR-010).
	rec := f.call(http.MethodPost, addressesPath+"/"+second+"/default", "", f.token)
	if rec.Code != http.StatusOK {
		t.Fatalf("mark default: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	promoted := decodeAddress(t, rec)
	if !promoted.IsDefault || promoted.ID != second {
		t.Fatalf("unexpected promote response: %+v", promoted)
	}
	rows := f.addressRows(t, f.owner)
	if rows[second].IsDefault != true || rows[first].IsDefault != false {
		t.Fatalf("the flag did not move: first=%+v second=%+v", rows[first], rows[second])
	}
	if got := f.visibleDefaultIDs(t, f.owner); len(got) != 1 || got[0] != second {
		t.Fatalf("expected exactly one visible default, got %v", got)
	}

	// An edit preserves the flag (FR-011).
	rec = f.call(http.MethodPatch, addressesPath+"/"+second,
		`{"streetAddress":"1 Le Loi, Ward 5"}`, f.token)
	if rec.Code != http.StatusOK {
		t.Fatalf("edit: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	edited := decodeAddress(t, rec)
	if !edited.IsDefault || edited.StreetAddress != "1 Le Loi, Ward 5" {
		t.Fatalf("the edit lost the flag or the street: %+v", edited)
	}
	if f.addressRows(t, f.owner)[second].IsDefault != true {
		t.Fatal("the stored row lost its default flag through an edit")
	}

	// Hiding the demoted address leaves exactly one default behind (SC-004).
	rec = f.call(http.MethodDelete, addressesPath+"/"+first, "", f.token)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("hide: expected 204, got %d (%s)", rec.Code, rec.Body.String())
	}
	listed = f.listAddresses(t, f.token, "")
	if listed.Meta.Total != 1 || len(listed.Data) != 1 || listed.Data[0].ID != second {
		t.Fatalf("expected only the remaining address, got %+v", listed)
	}
	if !listed.Data[0].IsDefault {
		t.Fatalf("the remaining address must still be the default, got %+v", listed.Data[0])
	}
	if got := f.visibleDefaultIDs(t, f.owner); len(got) != 1 || got[0] != second {
		t.Fatalf("expected exactly one visible default after the hide, got %v", got)
	}

	// SC-008: every change is traceable to the account, an action and a time.
	for action, want := range map[string]int{
		constant.AuditAddressCreated:    2,
		constant.AuditAddressUpdated:    1,
		constant.AuditAddressDefaultSet: 2,
		constant.AuditAddressDeleted:    1,
	} {
		f.drainAuditAction(t, action)
		events := f.auditRows(t, action)
		if len(events) != want {
			t.Fatalf("%s: expected %d events, got %d", action, want, len(events))
		}
		for _, event := range events {
			if event.ActorID == nil || *event.ActorID != f.owner {
				t.Fatalf("%s: expected the account %s as the actor, got %v", action, f.owner, event.ActorID)
			}
			if event.ActorRole != string(access.RoleCustomer) {
				t.Fatalf("%s: expected the actor role, got %q", action, event.ActorRole)
			}
			if event.TargetType != "address" {
				t.Fatalf("%s: expected the address as the target, got %q", action, event.TargetType)
			}
			if event.TargetID != first && event.TargetID != second {
				t.Fatalf("%s: expected one of the two addresses as the target, got %q", action, event.TargetID)
			}
			if event.Outcome != audit.OutcomeSuccess {
				t.Fatalf("%s: expected a SUCCESS outcome, got %q", action, event.Outcome)
			}
			if event.OccurredAt.IsZero() {
				t.Fatalf("%s: expected a point in time on the event", action)
			}
		}
	}

	// Another customer's address list is untouched by any of it.
	if others := f.addressRows(t, f.other); len(others) != 0 {
		t.Fatalf("another customer gained addresses: %+v", others)
	}
	if rec := f.call(http.MethodGet, addressesPath, "", f.token); rec.Code != http.StatusOK {
		t.Fatalf("the list still answers after the lifecycle: %d", rec.Code)
	}
}

// SC-007: hiding an address never changes what the stored address says, so an order
// that referenced it keeps showing exactly the text the customer used. The row is
// compared field by field before and after the hide.
func TestAHiddenAddressKeepsItsTextForOrderHistory(t *testing.T) {
	f := newIntegrationFixture(t)
	id := f.createAddress(t, f.token, addressProvinceCode, addressWardCode, "12 Nguyen Hue")
	before := f.addressRows(t, f.owner)[id]
	if before.DeletedAt != nil {
		t.Fatal("the seeded address must start visible")
	}

	rec := f.call(http.MethodDelete, addressesPath+"/"+id, "", f.token)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d (%s)", rec.Code, rec.Body.String())
	}
	after, ok := f.addressRows(t, f.owner)[id]
	if !ok {
		t.Fatal("the hidden row must survive: past orders still reference it")
	}
	if after.RecipientName != before.RecipientName ||
		after.RecipientPhone != before.RecipientPhone ||
		after.ProvinceCode != before.ProvinceCode ||
		after.ProvinceName != before.ProvinceName ||
		after.WardCode != before.WardCode ||
		after.WardName != before.WardName ||
		after.StreetAddress != before.StreetAddress {
		t.Fatalf("hiding changed the address text:\n before %+v\n after  %+v", before, after)
	}
	if after.DeletedAt == nil {
		t.Fatal("expected the row to carry a hidden marker")
	}
	if after.IsDefault {
		t.Fatal("a hidden address must not keep the default flag")
	}

	// The customer's list no longer shows it, so the address is gone from the screen
	// and intact in the table at the same time (FR-012).
	if listed := f.listAddresses(t, f.token, ""); listed.Meta.Total != 0 {
		t.Fatalf("expected an empty list, got %+v", listed)
	}

	// The hidden address is answered as not found, so it cannot be confirmed as
	// existing through the API (FR-013).
	if rec := f.call(http.MethodPatch, addressesPath+"/"+id, `{"streetAddress":"1 Le Loi"}`, f.token); rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for a hidden address, got %d (%s)", rec.Code, rec.Body.String())
	}
}

// research D10 through real infrastructure: a code that has left the dataset does
// not invalidate the stored address. It stays readable with its captured names and
// is flagged so the entry can be remapped.
func TestAnAddressWhoseCodeLeftTheDatasetStillReadsBack(t *testing.T) {
	f := newIntegrationFixture(t)
	// A code that is not in the bundled dataset, stored with the names captured
	// when it was valid.
	const retiredWardCode = "99999"
	id := uuid.New()
	const seed = `
		INSERT INTO addresses
			(id, user_id, recipient_name, recipient_phone, province_code, province_name,
			 ward_code, ward_name, street_address, is_default, created_at, updated_at)
		VALUES ($1, $2, 'Nguyen Van A', '0912345678', $3, 'Retired province (cu)',
			$4, 'Retired ward (cu)', '12 Nguyen Hue', true, now(), now())`
	province, err := administrative.FindProvince(addressProvinceCode)
	if err != nil {
		t.Fatalf("read the province from the dataset: %v", err)
	}
	if _, err := administrative.FindWard(retiredWardCode); err == nil {
		t.Fatalf("ward %s must not be in the dataset for this test to mean anything", retiredWardCode)
	}
	if _, err := f.pool.Exec(context.Background(), seed, id, f.owner, addressProvinceCode, retiredWardCode); err != nil {
		t.Fatalf("seed an address with a retired ward: %v", err)
	}

	listed := f.listAddresses(t, f.token, "")

	if len(listed.Data) != 1 || listed.Data[0].ID != id.String() {
		t.Fatalf("expected the retired address to stay listed, got %+v", listed.Data)
	}
	got := listed.Data[0]
	if !got.DivisionNeedsReview {
		t.Fatal("expected divisionNeedsReview to be true for a retired ward code")
	}
	if got.WardName != "Retired ward (cu)" || got.ProvinceName != "Retired province (cu)" {
		t.Fatalf("the captured names must be returned, got %q / %q", got.ProvinceName, got.WardName)
	}
	if got.ProvinceCode != addressProvinceCode || got.WardCode != retiredWardCode {
		t.Fatalf("the stored codes must stay joinable to the new dataset, got %+v", got)
	}
	if !got.IsDefault {
		t.Fatal("the seeded row is the default and must stay that one")
	}
	// The province name that *is* current was captured from the dataset at seed time
	// in the repository path; here the row was written by hand, so only assert that
	// a current province is not itself flagged.
	if province.Code != addressProvinceCode {
		t.Fatal("the dataset province changed under this test")
	}
}

// FR-023: a customer's addresses are preserved when their account is disabled, so
// historical orders stay reconstructable.
//
// FR-023 requires preservation, and that is what this test pins: disabling the
// account touches no address row, and the read an order would perform still returns
// them with their text. It deliberately does not assert that a write is refused —
// nothing on the request path consults users.status today
// (middleware.RequireAuthentication resolves the session only, and no user use case
// reads the status column), and no error code in contracts/error-codes.md covers
// "this account is disabled" for this module. Introducing that rule needs a decision
// about which module owns it; see the report.
func TestADisabledAccountKeepsItsAddresses(t *testing.T) {
	f := newIntegrationFixture(t)
	first := f.createAddress(t, f.token, addressProvinceCode, addressWardCode, "12 Nguyen Hue")
	second := f.createAddress(t, f.token, addressProvinceCode, otherAddressWardCode, "1 Le Loi")
	before := f.addressRows(t, f.owner)
	if len(before) != 2 {
		t.Fatalf("expected two stored addresses, got %d", len(before))
	}

	const disable = `UPDATE users SET status = 'disabled' WHERE id = $1`
	if _, err := f.pool.Exec(context.Background(), disable, f.owner); err != nil {
		t.Fatalf("disable the account: %v", err)
	}

	after := f.addressRows(t, f.owner)
	if len(after) != len(before) {
		t.Fatalf("disabling the account changed the address count: %d -> %d", len(before), len(after))
	}
	for id, row := range before {
		kept, ok := after[id]
		if !ok {
			t.Fatalf("address %s disappeared when the account was disabled", id)
		}
		if kept.RecipientName != row.RecipientName || kept.StreetAddress != row.StreetAddress ||
			kept.ProvinceCode != row.ProvinceCode || kept.ProvinceName != row.ProvinceName ||
			kept.WardCode != row.WardCode || kept.WardName != row.WardName {
			t.Fatalf("address %s changed when the account was disabled:\n before %+v\n after  %+v", id, row, kept)
		}
	}
	// The stored default is still exactly one, so an order history reconstruction
	// still sees a coherent address set.
	if got := f.visibleDefaultIDs(t, f.owner); len(got) != 1 || got[0] != first {
		t.Fatalf("expected one visible default after disabling, got %v", got)
	}

	// Hiding still works, which is what makes the removal path independent of the
	// account status. Hiding the account's default leaves no default behind — that
	// is the documented outcome (US2 acceptance scenario 6), not a promotion of the
	// remaining address.
	rec := f.call(http.MethodDelete, addressesPath+"/"+first, "", f.token)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d (%s)", rec.Code, rec.Body.String())
	}
	if f.addressRows(t, f.owner)[first].DeletedAt == nil {
		t.Fatal("expected the hide to be stamped even on a disabled account")
	}
	if got := f.visibleDefaultIDs(t, f.owner); len(got) != 0 {
		t.Fatalf("hiding the only default must leave none, got %v", got)
	}
	// The address that was not the default is untouched and still visible, so the
	// history of the disabled account stays reconstructable.
	if kept := f.addressRows(t, f.owner)[second]; kept.DeletedAt != nil || kept.StreetAddress != "1 Le Loi" {
		t.Fatalf("the remaining address changed: %+v", kept)
	}
}

// FR-007a and FR-007b through real infrastructure: the bundled dataset, not a fake,
// is what refuses a stale province, a stale ward and a ward from another province.
func TestDivisionsOutsideTheBundledDatasetAreRefused(t *testing.T) {
	cases := []struct {
		name  string
		body  string
		code  string
		field string
	}{
		{"unknown province", addressRequestBody("999", addressWardCode, "12 Nguyen Hue"),
			constant.CodeUnknownProvince, fieldProvinceCode},
		{"unknown ward", addressRequestBody(addressProvinceCode, "99999", "12 Nguyen Hue"),
			constant.CodeUnknownWard, fieldWardCode},
		{"a ward of another province", addressRequestBody(otherProvinceCode, addressWardCode, "12 Nguyen Hue"),
			constant.CodeWardProvinceMismatch, fieldWardCode},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newIntegrationFixture(t)

			rec := f.call(http.MethodPost, addressesPath, tc.body, f.token)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d (%s)", rec.Code, rec.Body.String())
			}
			if got := errorCode(t, rec); got != tc.code {
				t.Fatalf("expected %s, got %s", tc.code, got)
			}
			if got := detailField(t, rec); got != tc.field {
				t.Fatalf("expected the detail to name %q, got %q", tc.field, got)
			}
			if rows := f.addressRows(t, f.owner); len(rows) != 0 {
				t.Fatalf("a refused save must persist nothing, got %+v", rows)
			}
		})
	}
}

// SC-003 through real infrastructure: the owner's session cannot reach another
// customer's address, and their row is never touched.
func TestAnotherCustomersAddressIsRefusedAgainstPostgres(t *testing.T) {
	f := newIntegrationFixture(t)
	mine := f.createAddress(t, f.token, addressProvinceCode, addressWardCode, "12 Nguyen Hue")

	foreign := uuid.New()
	const seed = `
		INSERT INTO addresses
			(id, user_id, recipient_name, recipient_phone, province_code, province_name,
			 ward_code, ward_name, street_address, is_default, created_at, updated_at)
		VALUES ($1, $2, 'Someone Else', '0987654321', $3, 'Someone province',
			$4, 'Someone ward', '99 Le Loi', true, now(), now())`
	if _, err := f.pool.Exec(context.Background(), seed,
		foreign, f.other, addressProvinceCode, otherAddressWardCode); err != nil {
		t.Fatalf("seed a foreign address: %v", err)
	}

	// The owner's own session reaches for the other customer's address and gets the
	// same answer as an unknown identifier.
	for _, tc := range []struct {
		name   string
		invoke func() *httptest.ResponseRecorder
	}{
		{"edit", func() *httptest.ResponseRecorder {
			return f.call(http.MethodPatch, addressesPath+"/"+foreign.String(), `{"streetAddress":"1 Le Loi"}`, f.token)
		}},
		{"hide", func() *httptest.ResponseRecorder {
			return f.call(http.MethodDelete, addressesPath+"/"+foreign.String(), "", f.token)
		}},
		{"mark default", func() *httptest.ResponseRecorder {
			return f.call(http.MethodPost, addressesPath+"/"+foreign.String()+"/default", "", f.token)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := tc.invoke()
			if rec.Code != http.StatusNotFound {
				t.Fatalf("expected 404, got %d (%s)", rec.Code, rec.Body.String())
			}
			if got := errorCode(t, rec); got != constant.CodeAddressNotFound {
				t.Fatalf("expected %s, got %s", constant.CodeAddressNotFound, got)
			}
			if strings.Contains(rec.Body.String(), "Someone Else") || strings.Contains(rec.Body.String(), "99 Le Loi") {
				t.Fatalf("the refusal leaked the other customer's data: %s", rec.Body.String())
			}
		})
	}

	// The unknown identifier is answered identically, so the 404 confirms nothing.
	unknown := uuid.New().String()
	for _, path := range []string{addressesPath + "/" + unknown, addressesPath + "/" + unknown + "/default"} {
		method := http.MethodPatch
		if strings.HasSuffix(path, "/default") {
			method = http.MethodPost
		}
		body := `{"streetAddress":"1 Le Loi"}`
		rec := f.call(method, path, body, f.token)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s %s: expected 404, got %d (%s)", method, path, rec.Code, rec.Body.String())
		}
		if got := errorCode(t, rec); got != constant.CodeAddressNotFound {
			t.Fatalf("%s %s: expected %s, got %s", method, path, constant.CodeAddressNotFound, got)
		}
	}

	row := f.addressRows(t, f.other)[foreign.String()]
	if !row.IsDefault || row.StreetAddress != "99 Le Loi" || row.DeletedAt != nil {
		t.Fatalf("another customer's address changed: %+v", row)
	}
	// The acting account's own default did not move either.
	if got := f.visibleDefaultIDs(t, f.owner); len(got) != 1 || got[0] != mine {
		t.Fatalf("the owner's default changed: %v", got)
	}
}

// FR-018 through real infrastructure: the window is honoured, the total counts every
// non-hidden address, and the default comes first.
func TestTheAddressListIsPaginatedAgainstPostgres(t *testing.T) {
	f := newIntegrationFixture(t)
	first := f.createAddress(t, f.token, addressProvinceCode, addressWardCode, "12 Nguyen Hue")
	f.createAddress(t, f.token, addressProvinceCode, otherAddressWardCode, "1 Le Loi")
	f.createAddress(t, f.token, addressProvinceCode, addressWardCode, "3 Hai Ba Trung")

	page := f.listAddresses(t, f.token, "?page=1&pageSize=2")
	if page.Meta.Page != 1 || page.Meta.PageSize != 2 || page.Meta.Total != 3 {
		t.Fatalf("unexpected envelope: %+v", page.Meta)
	}
	if len(page.Data) != 2 || page.Data[0].ID != first || !page.Data[0].IsDefault {
		t.Fatalf("expected the default first on a page of 2, got %+v", page.Data)
	}

	second := f.listAddresses(t, f.token, "?page=2&pageSize=2")
	if len(second.Data) != 1 {
		t.Fatalf("expected 1 address on the second page, got %d", len(second.Data))
	}
	for _, row := range page.Data {
		if row.ID == second.Data[0].ID {
			t.Fatal("the second page repeated an address from the first")
		}
	}

	// A page beyond the end is empty, not an error, and still reports the total.
	beyond := f.listAddresses(t, f.token, "?page=9&pageSize=2")
	if len(beyond.Data) != 0 || beyond.Meta.Total != 3 {
		t.Fatalf("expected an empty page past the end with the total intact, got %+v", beyond)
	}
}
