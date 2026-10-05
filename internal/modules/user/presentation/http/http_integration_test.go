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
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/application/implement"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/application/interface"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/application/mapper"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/domain/constant"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/domain/model"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/infrastructure/implement/auditor"
	userpostgres "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/infrastructure/implement/postgres"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/access"
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
	profiles := implement.New(implement.Service{
		Profiles: userpostgres.NewProfileRepository(pool),
		Audit:    auditor.New(writer),
		Mapper:   mapper.New(nil),
	})

	owner := seedAccount(t, pool, "owner@example.com")
	other := seedAccount(t, pool, "other@example.com")

	token, _, err := authtoken.NewAccessIssuer(integrationSecret, integrationTokenTTL).
		Issue(owner, access.RoleCustomer)
	if err != nil {
		t.Fatalf("issue access token: %v", err)
	}

	handler := New(
		profileService{stubService: newStub(access.RoleCustomer), profiles: profiles},
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
	deadline := time.Now().Add(5 * time.Second)
	for {
		var pending int
		if err := f.pool.QueryRow(context.Background(),
			"SELECT count(*) FROM audit_logs WHERE action = $1", constant.AuditProfileUpdated).
			Scan(&pending); err != nil {
			t.Fatalf("count the audit rows: %v", err)
		}
		if pending > 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the audit writer never persisted the profile update")
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
