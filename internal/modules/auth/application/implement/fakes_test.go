package implement

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	appdto "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/application/dto"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/application/interface"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/model"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/access"
)

type fakeUsers struct {
	byEmail map[string]*model.Account
	byID    map[uuid.UUID]*model.Account
}

func newFakeUsers() *fakeUsers {
	return &fakeUsers{byEmail: map[string]*model.Account{}, byID: map[uuid.UUID]*model.Account{}}
}

func (f *fakeUsers) Create(_ context.Context, a *model.Account) error {
	if _, ok := f.byEmail[a.Email]; ok {
		return domainerr.ErrEmailTaken
	}
	f.byEmail[a.Email] = a
	f.byID[a.ID] = a
	return nil
}

func (f *fakeUsers) UpsertAdmin(_ context.Context, a *model.Account) error {
	f.byEmail[a.Email] = a
	f.byID[a.ID] = a
	return nil
}

func (f *fakeUsers) ByEmail(_ context.Context, email string) (*model.Account, error) {
	a, ok := f.byEmail[email]
	if !ok {
		return nil, domainerr.ErrUserNotFound
	}
	return a, nil
}

func (f *fakeUsers) ByID(_ context.Context, id uuid.UUID) (*model.Account, error) {
	a, ok := f.byID[id]
	if !ok {
		return nil, domainerr.ErrUserNotFound
	}
	return a, nil
}

func (f *fakeUsers) Activate(_ context.Context, id uuid.UUID) error {
	if a, ok := f.byID[id]; ok {
		a.Status = constant.StatusActive
	}
	return nil
}

func (f *fakeUsers) UpdatePassword(_ context.Context, id uuid.UUID, hash string) error {
	if a, ok := f.byID[id]; ok {
		a.PasswordHash = hash
	}
	return nil
}

type fakeSessions struct {
	byHash map[string]*model.Session
}

func newFakeSessions() *fakeSessions { return &fakeSessions{byHash: map[string]*model.Session{}} }

func (f *fakeSessions) Create(_ context.Context, s *model.Session) error {
	f.byHash[s.RefreshTokenHash] = s
	return nil
}

func (f *fakeSessions) ByTokenHash(_ context.Context, hash string) (*model.Session, error) {
	s, ok := f.byHash[hash]
	if !ok {
		return nil, domainerr.ErrSessionNotFound
	}
	return s, nil
}

func (f *fakeSessions) Rotate(_ context.Context, oldID uuid.UUID, next *model.Session) error {
	now := time.Now().UTC()
	for _, s := range f.byHash {
		if s.ID == oldID {
			s.RevokedAt = &now
		}
	}
	f.byHash[next.RefreshTokenHash] = next
	return nil
}

func (f *fakeSessions) Revoke(_ context.Context, id uuid.UUID) error {
	now := time.Now().UTC()
	for _, s := range f.byHash {
		if s.ID == id {
			s.RevokedAt = &now
		}
	}
	return nil
}

func (f *fakeSessions) RevokeAllForUser(_ context.Context, userID uuid.UUID) error {
	now := time.Now().UTC()
	for _, s := range f.byHash {
		if s.UserID == userID {
			s.RevokedAt = &now
		}
	}
	return nil
}

type fakeResets struct {
	byHash map[string]*model.ResetRequest
}

func newFakeResets() *fakeResets { return &fakeResets{byHash: map[string]*model.ResetRequest{}} }

func (f *fakeResets) Create(_ context.Context, r *model.ResetRequest) error {
	now := time.Now().UTC()
	for _, existing := range f.byHash {
		if existing.UserID == r.UserID && existing.UsedAt == nil {
			existing.UsedAt = &now
		}
	}
	f.byHash[r.TokenHash] = r
	return nil
}

func (f *fakeResets) ByTokenHash(_ context.Context, hash string) (*model.ResetRequest, error) {
	r, ok := f.byHash[hash]
	if !ok {
		return nil, domainerr.ErrResetNotFound
	}
	return r, nil
}

func (f *fakeResets) Consume(_ context.Context, id uuid.UUID) error {
	now := time.Now().UTC()
	for _, r := range f.byHash {
		if r.ID == id {
			r.UsedAt = &now
		}
	}
	return nil
}

type fakeOTP struct {
	mu          sync.Mutex
	hasher      appinterface.PasswordHasher
	maxAttempts int
	entries     map[string]*otpEntry
	disarms     int
}

type otpEntry struct {
	hash     string
	attempts int
	blocked  bool
	cooldown bool
}

func newFakeOTP(hasher appinterface.PasswordHasher, maxAttempts int) *fakeOTP {
	return &fakeOTP{hasher: hasher, maxAttempts: maxAttempts, entries: map[string]*otpEntry{}}
}

func (f *fakeOTP) Issue(_ context.Context, email, otpHash string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.entries[email] = &otpEntry{hash: otpHash, cooldown: true}
	return nil
}

// DisarmCooldown models the port: only the resend marker is cleared, so the
// code and the block marker survive a failed delivery.
func (f *fakeOTP) DisarmCooldown(_ context.Context, email string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.disarms++
	if e, ok := f.entries[email]; ok {
		e.cooldown = false
	}
	return nil
}

// disarmCount reports how many times the cooldown was disarmed.
func (f *fakeOTP) disarmCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.disarms
}

func (f *fakeOTP) CanResend(_ context.Context, email string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	e, ok := f.entries[email]
	if !ok {
		return nil
	}
	if e.blocked {
		return domainerr.ErrOTPBlocked
	}
	if e.cooldown {
		return domainerr.ErrResendCooldown
	}
	return nil
}

func (f *fakeOTP) Verify(_ context.Context, email, otp string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	e, ok := f.entries[email]
	if !ok {
		return domainerr.ErrOTPExpired
	}
	if e.blocked {
		return domainerr.ErrOTPBlocked
	}
	okMatch, err := f.hasher.Verify(e.hash, otp)
	if err != nil {
		return domainerr.ErrOTPInvalid
	}
	if okMatch {
		delete(f.entries, email)
		return nil
	}
	e.attempts++
	if e.attempts >= f.maxAttempts {
		e.blocked = true
		return domainerr.ErrOTPTooManyAttempts
	}
	return domainerr.ErrOTPInvalid
}

func (f *fakeOTP) Invalidate(_ context.Context, email string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.entries, email)
	return nil
}

type fakeBlacklist struct {
	jtis    map[string]bool
	userMin map[uuid.UUID]int64
}

func newFakeBlacklist() *fakeBlacklist {
	return &fakeBlacklist{jtis: map[string]bool{}, userMin: map[uuid.UUID]int64{}}
}

func (f *fakeBlacklist) RevokeJTI(_ context.Context, jti string, _ time.Duration) error {
	f.jtis[jti] = true
	return nil
}

func (f *fakeBlacklist) IsJTIRevoked(_ context.Context, jti string) (bool, error) {
	return f.jtis[jti], nil
}

func (f *fakeBlacklist) RevokeUserBefore(_ context.Context, userID uuid.UUID, at time.Time, _ time.Duration) error {
	f.userMin[userID] = at.UTC().Unix()
	return nil
}

func (f *fakeBlacklist) IsIssuedBefore(_ context.Context, userID uuid.UUID, iat time.Time) (bool, error) {
	earliest, ok := f.userMin[userID]
	if !ok {
		return false, nil
	}
	return iat.UTC().Unix() < earliest, nil
}

type fakeGuard struct {
	failures map[string]int
	blocked  map[string]bool
	max      int
}

func newFakeGuard(limit int) *fakeGuard {
	return &fakeGuard{failures: map[string]int{}, blocked: map[string]bool{}, max: limit}
}

func (f *fakeGuard) Blocked(_ context.Context, source string) (bool, error) {
	return f.blocked[source], nil
}

func (f *fakeGuard) RecordFailure(_ context.Context, source string) (bool, error) {
	f.failures[source]++
	if f.failures[source] >= f.max {
		f.blocked[source] = true
		return true, nil
	}
	return false, nil
}

func (f *fakeGuard) Reset(_ context.Context, source string) error {
	delete(f.failures, source)
	delete(f.blocked, source)
	return nil
}

type fakeEmail struct {
	mu       sync.Mutex
	messages []string
	lastBody string
	sends    int
	// remaining/failErr make the next n sends fail; build makes every send fail
	// with an error built from the message body.
	remaining int
	failErr   error
	build     func(body string) error
}

var _ appinterface.EmailSender = (*fakeEmail)(nil)

// failNext makes the next n sends fail with err; every send after them succeeds.
func (f *fakeEmail) failNext(n int, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.remaining, f.failErr, f.build = n, err, nil
}

// failAlwaysWith makes every send fail with the error build returns for the
// message body, so a test can simulate a provider that quotes what it was sent.
func (f *fakeEmail) failAlwaysWith(build func(body string) error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.remaining, f.failErr, f.build = 0, nil, build
}

// Send records a delivered message. A refused send delivers nothing, so it is
// never recorded and never counted as a message the customer could hold.
func (f *fakeEmail) Send(_ context.Context, to, _ string, body string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sends++
	if f.build != nil {
		return f.build(body)
	}
	if f.remaining > 0 {
		f.remaining--
		return f.failErr
	}
	f.messages = append(f.messages, to)
	f.lastBody = body
	return nil
}

func (f *fakeEmail) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.messages)
}

func (f *fakeEmail) attempts() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.sends
}

// auditEvent is one recorded audit entry, kept whole so a test can assert on
// every field a trace carries.
type auditEvent struct {
	action     string
	outcome    string
	actorRole  string
	targetType string
	targetID   string
	metadata   map[string]any
}

type fakeAuditor struct {
	mu     sync.Mutex
	events []auditEvent
}

func (f *fakeAuditor) Record(_ context.Context, action, outcome string, _ *uuid.UUID, actorRole, targetType, targetID string, metadata map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, auditEvent{
		action:     action,
		outcome:    outcome,
		actorRole:  actorRole,
		targetType: targetType,
		targetID:   targetID,
		metadata:   metadata,
	})
}

func (f *fakeAuditor) has(action string) bool {
	_, ok := f.find(action)
	return ok
}

func (f *fakeAuditor) find(action string) (auditEvent, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, e := range f.events {
		if e.action == action {
			return e, true
		}
	}
	return auditEvent{}, false
}

// traces renders every recorded audit entry, so a test can assert that no trace
// carries a value it must not hold (FR-012).
func (f *fakeAuditor) traces() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.events))
	for _, e := range f.events {
		out = append(out, fmt.Sprintf("%s %s %s %s %s %v", e.action, e.outcome, e.actorRole, e.targetType, e.targetID, e.metadata))
	}
	return strings.Join(out, "\n")
}

type testHasher struct{}

func (testHasher) Hash(password string) (string, error) { return "h:" + password, nil }
func (testHasher) Verify(encoded, password string) (bool, error) {
	return encoded == "h:"+password, nil
}

type testTokens struct{}

func (testTokens) Issue(userID uuid.UUID, role access.Role) (string, time.Time, error) {
	return fmt.Sprintf("access:%s:%s", userID, role), time.Now().Add(time.Minute), nil
}
func (testTokens) Parse(raw string) (appinterface.Claims, error) {
	parts := strings.Split(raw, ":")
	if len(parts) != 3 {
		return appinterface.Claims{}, domainerr.ErrInvalidToken
	}
	id, err := uuid.Parse(parts[1])
	if err != nil {
		return appinterface.Claims{}, domainerr.ErrInvalidToken
	}
	return appinterface.Claims{Subject: id, Role: access.Role(parts[2]), ID: raw, IssuedAt: time.Now()}, nil
}
func (testTokens) TTL() time.Duration { return time.Minute }

type testRefresh struct{}

func (testRefresh) Generate() (string, string, error) {
	id := uuid.NewString()
	return "refresh:" + id, "hash:" + id, nil
}
func (testRefresh) Hash(raw string) string { return "hash:" + strings.TrimPrefix(raw, "refresh:") }

type fakeTx struct {
	mu    sync.Mutex
	calls int
	err   error
}

func (f *fakeTx) WithinTx(ctx context.Context, fn func(context.Context) error) error {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	return fn(ctx)
}

func (f *fakeTx) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

type harness struct {
	svc       *Service
	users     *fakeUsers
	sessions  *fakeSessions
	resets    *fakeResets
	otp       *fakeOTP
	blacklist *fakeBlacklist
	guard     *fakeGuard
	email     *fakeEmail
	audit     *fakeAuditor
	tx        *fakeTx
	logs      *bytes.Buffer
}

func newHarness() *harness {
	h := &harness{logs: &bytes.Buffer{}}
	hasher := testHasher{}
	h.users = newFakeUsers()
	h.sessions = newFakeSessions()
	h.resets = newFakeResets()
	h.otp = newFakeOTP(hasher, 3)
	h.blacklist = newFakeBlacklist()
	h.guard = newFakeGuard(10)
	h.email = &fakeEmail{}
	h.audit = &fakeAuditor{}
	h.tx = &fakeTx{}
	h.svc = New(Service{
		Users:         h.users,
		Sessions:      h.sessions,
		Resets:        h.resets,
		OTP:           h.otp,
		Blacklist:     h.blacklist,
		Guard:         h.guard,
		Hasher:        hasher,
		Access:        testTokens{},
		RefreshTokens: testRefresh{},
		Email:         h.email,
		Audit:         h.audit,
		Tx:            h.tx,
		Logger:        slog.New(slog.NewJSONHandler(h.logs, nil)),
		Config:        Config{RefreshTokenTTL: time.Hour, PasswordResetTTL: time.Hour},
	})
	return h
}

func (h *harness) registerActive(email, password string) {
	_ = h.svc.Register(context.Background(), appdto.RegisterInput{Email: email, Password: password})
	account, _ := h.users.ByEmail(context.Background(), model.NormalizeEmail(email))
	_ = h.users.Activate(context.Background(), account.ID)
}
