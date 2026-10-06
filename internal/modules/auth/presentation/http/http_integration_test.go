//go:build integration

package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/application/implement"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/application/interface"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/infrastructure/implement/email"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/infrastructure/implement/postgres"
	authredis "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/infrastructure/implement/redis"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/infrastructure/implement/token"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/cache"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/config"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/database"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/database/migrate"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/testsupport"
)

type noopAuditor struct{}

func (noopAuditor) Record(context.Context, string, string, *uuid.UUID, string, string, string, map[string]any) {
}

var _ appinterface.Auditor = noopAuditor{}

func integrationRouter(t *testing.T) (http.Handler, *email.FakeSender) {
	t.Helper()
	dsn := testsupport.PostgresDSN(t)
	ctx := context.Background()

	runner, err := migrate.New(dsn, 30*time.Second)
	if err != nil {
		t.Fatalf("migrate.New: %v", err)
	}
	defer runner.Close()
	if err := runner.Up(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(pool.Close)

	server, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	t.Cleanup(server.Close)
	redisCache, err := cache.New(ctx, config.RedisConfig{Addr: server.Addr()})
	if err != nil {
		t.Fatalf("cache: %v", err)
	}
	t.Cleanup(func() { _ = redisCache.Close() })

	sender := &email.FakeSender{}
	svc := implement.New(implement.Service{
		Users:         postgres.NewUserRepository(pool),
		Sessions:      postgres.NewSessionRepository(pool),
		Resets:        postgres.NewResetRepository(pool),
		OTP:           authredis.NewOTPStore(redisCache, token.Hasher{}, 3, 15*time.Minute, time.Minute, time.Minute),
		Blacklist:     authredis.NewBlacklistStore(redisCache),
		Guard:         authredis.NewLoginGuard(redisCache, 10, 15*time.Minute, 15*time.Minute),
		Hasher:        token.Hasher{},
		Access:        token.NewAccessIssuer("0123456789abcdef0123456789abcdef", 15*time.Minute),
		RefreshTokens: token.Generator{},
		Email:         sender,
		Audit:         noopAuditor{},
		Tx:            &database.DB{Pool: pool},
		Config:        implement.Config{RefreshTokenTTL: 45 * 24 * time.Hour, PasswordResetTTL: 30 * time.Minute},
	})
	h := New(svc, noopAuditor{}, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	return h.Router(config.AuthConfig{LoginRatePerMinute: 100, FlowRatePerMinute: 100}), sender
}

func post(handler http.Handler, path, body, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func lastFieldText(body string) string {
	fields := strings.Fields(body)
	if len(fields) == 0 {
		return ""
	}
	return fields[len(fields)-1]
}

type tokenPair struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
}

func login(t *testing.T, handler http.Handler, email, password string) tokenPair {
	t.Helper()
	rec := post(handler, "/login", `{"email":"`+email+`","password":"`+password+`"}`, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("login: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var body struct {
		Data tokenPair `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode login: %v", err)
	}
	return body.Data
}

func registerAndVerify(t *testing.T, handler http.Handler, sender *email.FakeSender, email, password string) {
	t.Helper()
	if rec := post(handler, "/register", `{"email":"`+email+`","password":"`+password+`"}`, ""); rec.Code != http.StatusAccepted {
		t.Fatalf("register: expected 202, got %d (%s)", rec.Code, rec.Body.String())
	}
	msg, ok := sender.Last()
	if !ok {
		t.Fatal("expected an OTP email")
	}
	otp := lastFieldText(msg.Body)
	if rec := post(handler, "/verify-email", `{"email":"`+email+`","otp":"`+otp+`"}`, ""); rec.Code != http.StatusOK {
		t.Fatalf("verify-email: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestAuthHTTPFlow(t *testing.T) {
	handler, sender := integrationRouter(t)
	const email = "flow@example.com"
	const password = "Str0ng!Pass"

	registerAndVerify(t, handler, sender, email, password)
	pair := login(t, handler, email, password)

	meReq := httptest.NewRequest(http.MethodGet, "/me", nil)
	meReq.Header.Set("Authorization", "Bearer "+pair.AccessToken)
	meRec := httptest.NewRecorder()
	handler.ServeHTTP(meRec, meReq)
	if meRec.Code != http.StatusOK {
		t.Fatalf("me: expected 200, got %d", meRec.Code)
	}

	if rec := post(handler, "/refresh", `{"refreshToken":"`+pair.RefreshToken+`"}`, ""); rec.Code != http.StatusOK {
		t.Fatalf("refresh: expected 200, got %d", rec.Code)
	}
	if rec := post(handler, "/refresh", `{"refreshToken":"`+pair.RefreshToken+`"}`, ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("refresh reuse: expected 401, got %d", rec.Code)
	}
}

func TestMultiDeviceSignOutIsolation(t *testing.T) {
	handler, sender := integrationRouter(t)
	const email = "multi@example.com"
	const password = "Str0ng!Pass"

	registerAndVerify(t, handler, sender, email, password)
	deviceA := login(t, handler, email, password)
	deviceB := login(t, handler, email, password)

	if rec := post(handler, "/logout", `{"refreshToken":"`+deviceA.RefreshToken+`"}`, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("logout: expected 204, got %d", rec.Code)
	}
	if rec := post(handler, "/refresh", `{"refreshToken":"`+deviceB.RefreshToken+`"}`, ""); rec.Code != http.StatusOK {
		t.Fatalf("other device should still work, got %d", rec.Code)
	}
}

func TestRefreshReplayRevokesEveryDevice(t *testing.T) {
	handler, sender := integrationRouter(t)
	const email = "replay@example.com"
	const password = "Str0ng!Pass"

	registerAndVerify(t, handler, sender, email, password)
	deviceA := login(t, handler, email, password)
	deviceB := login(t, handler, email, password)

	if rec := post(handler, "/refresh", `{"refreshToken":"`+deviceA.RefreshToken+`"}`, ""); rec.Code != http.StatusOK {
		t.Fatalf("first refresh: expected 200, got %d", rec.Code)
	}
	rec := post(handler, "/refresh", `{"refreshToken":"`+deviceA.RefreshToken+`"}`, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("replay: expected 401, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "AUTH_REFRESH_REUSED") {
		t.Fatalf("expected AUTH_REFRESH_REUSED, got %s", rec.Body.String())
	}
	if rec := post(handler, "/refresh", `{"refreshToken":"`+deviceB.RefreshToken+`"}`, ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("replay must revoke the other device too, got %d", rec.Code)
	}
}

func TestChangePasswordRevokesEverySession(t *testing.T) {
	handler, sender := integrationRouter(t)
	const email = "change@example.com"
	const password = "Str0ng!Pass"

	registerAndVerify(t, handler, sender, email, password)
	deviceA := login(t, handler, email, password)
	deviceB := login(t, handler, email, password)

	body := `{"currentPassword":"` + password + `","newPassword":"Even5tronger!","refreshToken":"` + deviceA.RefreshToken + `"}`
	rec := post(handler, "/password/change", body, deviceA.AccessToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("change password: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if rec := post(handler, "/refresh", `{"refreshToken":"`+deviceB.RefreshToken+`"}`, ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected every session to be revoked, got %d", rec.Code)
	}
}

// TestAFailedDeliveryLeavesTheCustomerAWorkingRecoveryPath exercises the whole
// story against real storage: the account is committed, the refusal is answered
// as a service problem naming the next step, and the new code that step promises
// can be requested in the same second - because the send that never happened did
// not consume the cooldown the real store had armed (FR-007, FR-024).
func TestAFailedDeliveryLeavesTheCustomerAWorkingRecoveryPath(t *testing.T) {
	handler, sender := integrationRouter(t)
	const email = "providerdown@example.com"
	const password = "Str0ng!Pass"

	sender.FailNext(1, fmt.Errorf("%w: the provider reported status 550", domainerr.ErrDeliveryConfiguration))

	rec := post(handler, "/register", `{"email":"`+email+`","password":"`+password+`"}`, "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("register: expected 503, got %d (%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "SERVICE_UNAVAILABLE") {
		t.Fatalf("expected the shared service-unavailable code, got %s", rec.Body.String())
	}
	if _, ok := sender.Last(); ok {
		t.Fatal("no message left the system, so none may be recorded as delivered")
	}

	// The answer told the customer they could request a new code, so this must not
	// be refused with the resend cooldown.
	rec = post(handler, "/resend-verification", `{"email":"`+email+`"}`, "")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("resend: expected 202, got %d (%s)", rec.Code, rec.Body.String())
	}
	msg, ok := sender.Last()
	if !ok {
		t.Fatal("expected the new code to be delivered")
	}
	if rec := post(handler, "/verify-email", `{"email":"`+email+`","otp":"`+lastFieldText(msg.Body)+`"}`, ""); rec.Code != http.StatusOK {
		t.Fatalf("verify-email: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
}
