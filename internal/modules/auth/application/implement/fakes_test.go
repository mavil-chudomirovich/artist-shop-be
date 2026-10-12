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

	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/application/interface"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/model"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/access"
)

type fakeUsers struct {
	mu      sync.Mutex
	byEmail map[string]*model.Account
	byID    map[uuid.UUID]*model.Account
	// createErr, when set, makes every Create fail with it, so a test can
	// exercise an account-store failure at confirmation time (FR-010).
	createErr error
}

func newFakeUsers() *fakeUsers {
	return &fakeUsers{byEmail: map[string]*model.Account{}, byID: map[uuid.UUID]*model.Account{}}
}

func (f *fakeUsers) Create(_ context.Context, a *model.Account) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.createErr != nil {
		return f.createErr
	}
	if _, ok := f.byEmail[a.Email]; ok {
		return domainerr.ErrEmailTaken
	}
	if a.Username != nil {
		for _, existing := range f.byEmail {
			if existing.Username != nil && strings.EqualFold(*existing.Username, *a.Username) {
				return domainerr.ErrUsernameTaken
			}
		}
	}
	f.byEmail[a.Email] = a
	f.byID[a.ID] = a
	return nil
}

func (f *fakeUsers) UpsertAdmin(_ context.Context, a *model.Account) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.byEmail[a.Email] = a
	f.byID[a.ID] = a
	return nil
}

func (f *fakeUsers) ByEmail(_ context.Context, email string) (*model.Account, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	a, ok := f.byEmail[email]
	if !ok {
		return nil, domainerr.ErrUserNotFound
	}
	return a, nil
}

func (f *fakeUsers) ByUsername(_ context.Context, username string) (*model.Account, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, a := range f.byEmail {
		if a.Username != nil && strings.EqualFold(*a.Username, strings.TrimSpace(username)) {
			return a, nil
		}
	}
	return nil, domainerr.ErrUserNotFound
}

func (f *fakeUsers) ByIdentifier(_ context.Context, identifier string) (*model.Account, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if a, ok := f.byEmail[strings.ToLower(strings.TrimSpace(identifier))]; ok {
		return a, nil
	}
	for _, a := range f.byEmail {
		if a.Username != nil && strings.EqualFold(*a.Username, strings.TrimSpace(identifier)) {
			return a, nil
		}
	}
	return nil, domainerr.ErrUserNotFound
}

func (f *fakeUsers) ByID(_ context.Context, id uuid.UUID) (*model.Account, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	a, ok := f.byID[id]
	if !ok {
		return nil, domainerr.ErrUserNotFound
	}
	return a, nil
}

func (f *fakeUsers) Activate(_ context.Context, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if a, ok := f.byID[id]; ok {
		a.Status = constant.StatusActive
	}
	return nil
}

func (f *fakeUsers) UpdatePassword(_ context.Context, id uuid.UUID, hash string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if a, ok := f.byID[id]; ok {
		a.PasswordHash = hash
	}
	return nil
}

// snapshotPasswords captures every account's password hash, so a rollback-capable
// test transaction can restore it when a use case fails and its writes must be
// undone.
func (f *fakeUsers) snapshotPasswords() map[uuid.UUID]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	snap := make(map[uuid.UUID]string, len(f.byID))
	for id, a := range f.byID {
		snap[id] = a.PasswordHash
	}
	return snap
}

// restorePasswords puts the snapshotted hashes back, mutating the stored
// accounts in place so any pointer a test still holds sees the restore.
func (f *fakeUsers) restorePasswords(snap map[uuid.UUID]string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for id, hash := range snap {
		if a, ok := f.byID[id]; ok {
			a.PasswordHash = hash
		}
	}
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
	// consumeConflict, when set, makes Consume report as if a concurrent
	// transaction had already marked the row used after the use case read it.
	// It models the race the conditional update defends against: the read sees
	// an open token, but the write finds it consumed.
	consumeConflict bool
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

// Consume mirrors the adapter: it is conditional, so a token can be consumed
// only once. A second consume - or one that lost the race to another
// transaction - affects no row and reports ErrResetInvalid (research D11).
func (f *fakeResets) Consume(_ context.Context, id uuid.UUID) error {
	if f.consumeConflict {
		return domainerr.ErrResetInvalid
	}
	now := time.Now().UTC()
	for _, r := range f.byHash {
		if r.ID != id {
			continue
		}
		if r.UsedAt != nil {
			return domainerr.ErrResetInvalid
		}
		r.UsedAt = &now
		return nil
	}
	return domainerr.ErrResetInvalid
}

type otpEntry struct {
	hash     string
	attempts int
	blocked  bool
	cooldown bool
}

// fakeRegistration is an in-memory appinterface.RegistrationStore. It models the
// temporary registration and its code state, replacing any previous record for
// the same email exactly as the Redis adapter does (FR-026).
type fakeRegistration struct {
	mu          sync.Mutex
	hasher      appinterface.PasswordHasher
	maxAttempts int
	records     map[string]*model.PendingRegistration
	codes       map[string]*otpEntry
	disarms     int
}

var _ appinterface.RegistrationStore = (*fakeRegistration)(nil)

func newFakeRegistration(hasher appinterface.PasswordHasher, maxAttempts int) *fakeRegistration {
	return &fakeRegistration{
		hasher:      hasher,
		maxAttempts: maxAttempts,
		records:     map[string]*model.PendingRegistration{},
		codes:       map[string]*otpEntry{},
	}
}

func (f *fakeRegistration) Save(_ context.Context, reg model.PendingRegistration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for id, existing := range f.records {
		if id != reg.RegistrationID && existing.Email == reg.Email {
			delete(f.records, id)
			delete(f.codes, id)
		}
	}
	cp := reg
	f.records[reg.RegistrationID] = &cp
	return nil
}

func (f *fakeRegistration) ByID(_ context.Context, registrationID string) (model.PendingRegistration, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	reg, ok := f.records[registrationID]
	if !ok {
		return model.PendingRegistration{}, domainerr.ErrRegistrationNotFound
	}
	return *reg, nil
}

func (f *fakeRegistration) Delete(_ context.Context, registrationID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.records, registrationID)
	delete(f.codes, registrationID)
	return nil
}

// Complete models the port: the code state is removed and the record is marked
// completed but retained, so a repeated confirmation reports success without a
// second account (FR-010, FR-011). It is idempotent.
func (f *fakeRegistration) Complete(_ context.Context, registrationID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.codes, registrationID)
	if reg, ok := f.records[registrationID]; ok {
		reg.Status = model.RegistrationStatusCompleted
	}
	return nil
}

func (f *fakeRegistration) IssueOTP(_ context.Context, registrationID, otpHash string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.codes[registrationID] = &otpEntry{hash: otpHash, cooldown: true}
	return nil
}

func (f *fakeRegistration) CanResend(_ context.Context, registrationID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	e, ok := f.codes[registrationID]
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

func (f *fakeRegistration) VerifyOTP(_ context.Context, registrationID, otp string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	e, ok := f.codes[registrationID]
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
		// The code is kept: it is removed when the registration is completed, so
		// a failed account insert can be retried with the same code (FR-010).
		e.attempts = 0
		return nil
	}
	e.attempts++
	if e.attempts >= f.maxAttempts {
		e.blocked = true
		return domainerr.ErrOTPTooManyAttempts
	}
	return domainerr.ErrOTPInvalid
}

func (f *fakeRegistration) InvalidateOTP(_ context.Context, registrationID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.codes, registrationID)
	return nil
}

func (f *fakeRegistration) DisarmCooldown(_ context.Context, registrationID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.disarms++
	if e, ok := f.codes[registrationID]; ok {
		e.cooldown = false
	}
	return nil
}

// disarmCount reports how many times a failed delivery disarmed the cooldown.
func (f *fakeRegistration) disarmCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.disarms
}

// count reports how many live temporary registrations are held.
func (f *fakeRegistration) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.records)
}

// idByEmail resolves the live reference for an address, so a test that could not
// read it from a failed start (which returns no reference) can still drive the
// follow-up calls.
func (f *fakeRegistration) idByEmail(email string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	for id, reg := range f.records {
		if reg.Email == email {
			return id
		}
	}
	return ""
}

// status reports the stored lifecycle state of a registration.
func (f *fakeRegistration) status(registrationID string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if reg, ok := f.records[registrationID]; ok {
		return reg.Status
	}
	return ""
}

// expireCode simulates the code's own lifetime elapsing without touching the
// registration record, so a test can assert the expired-code branch (FR-007).
func (f *fakeRegistration) expireCode(registrationID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.codes, registrationID)
}

// fakeResetOTP is an in-memory appinterface.PasswordResetOTPStore. It models the
// reset code challenge and its brute-force state keyed by the normalized email,
// exactly as the Redis adapter does.
type fakeResetOTP struct {
	mu          sync.Mutex
	hasher      appinterface.PasswordHasher
	maxAttempts int
	codes       map[string]*otpEntry
	disarms     int
}

var _ appinterface.PasswordResetOTPStore = (*fakeResetOTP)(nil)

func newFakeResetOTP(hasher appinterface.PasswordHasher, maxAttempts int) *fakeResetOTP {
	return &fakeResetOTP{
		hasher:      hasher,
		maxAttempts: maxAttempts,
		codes:       map[string]*otpEntry{},
	}
}

func (f *fakeResetOTP) Issue(_ context.Context, email, otpHash string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.codes[email] = &otpEntry{hash: otpHash, cooldown: true}
	return nil
}

func (f *fakeResetOTP) CanResend(_ context.Context, email string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	e, ok := f.codes[email]
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

func (f *fakeResetOTP) Verify(_ context.Context, email, otp string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	e, ok := f.codes[email]
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
		// Mirror the adapter: the code is kept so a failed token write can be
		// retried with the same code; Invalidate removes it after the write.
		e.attempts = 0
		return nil
	}
	e.attempts++
	if e.attempts >= f.maxAttempts {
		e.blocked = true
		return domainerr.ErrOTPTooManyAttempts
	}
	return domainerr.ErrOTPInvalid
}

func (f *fakeResetOTP) Invalidate(_ context.Context, email string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.codes, email)
	return nil
}

func (f *fakeResetOTP) DisarmCooldown(_ context.Context, email string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.disarms++
	if e, ok := f.codes[email]; ok {
		e.cooldown = false
	}
	return nil
}

// disarmCount reports how many times a failed delivery disarmed the cooldown.
func (f *fakeResetOTP) disarmCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.disarms
}

// expireCode simulates the code's own lifetime elapsing without touching the
// rest of the challenge state, so a test can assert the expired-code branch.
func (f *fakeResetOTP) expireCode(email string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.codes, email)
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

// rollbackTx is a UnitOfWork test double that snapshots the account store before
// running fn and restores it when fn fails, so a test can prove that a refused
// use case rolls its writes back (research D11).
type rollbackTx struct {
	users *fakeUsers
	calls int
}

func (t *rollbackTx) WithinTx(ctx context.Context, fn func(context.Context) error) error {
	t.calls++
	snap := t.users.snapshotPasswords()
	if err := fn(ctx); err != nil {
		t.users.restorePasswords(snap)
		return err
	}
	return nil
}

type harness struct {
	svc          *Service
	users        *fakeUsers
	sessions     *fakeSessions
	resets       *fakeResets
	registration *fakeRegistration
	resetOTP     *fakeResetOTP
	blacklist    *fakeBlacklist
	guard        *fakeGuard
	email        *fakeEmail
	audit        *fakeAuditor
	tx           *fakeTx
	logs         *bytes.Buffer
}

func newHarness() *harness {
	h := &harness{logs: &bytes.Buffer{}}
	hasher := testHasher{}
	h.users = newFakeUsers()
	h.sessions = newFakeSessions()
	h.resets = newFakeResets()
	h.registration = newFakeRegistration(hasher, 3)
	h.resetOTP = newFakeResetOTP(hasher, 3)
	h.blacklist = newFakeBlacklist()
	h.guard = newFakeGuard(10)
	h.email = &fakeEmail{}
	h.audit = &fakeAuditor{}
	h.tx = &fakeTx{}
	h.svc = New(Service{
		Users:         h.users,
		Sessions:      h.sessions,
		Resets:        h.resets,
		Registration:  h.registration,
		ResetOTP:      h.resetOTP,
		Blacklist:     h.blacklist,
		Guard:         h.guard,
		Hasher:        hasher,
		Access:        testTokens{},
		RefreshTokens: testRefresh{},
		Email:         h.email,
		Audit:         h.audit,
		Tx:            h.tx,
		Logger:        slog.New(slog.NewJSONHandler(h.logs, nil)),
		Config:        Config{RefreshTokenTTL: time.Hour, PasswordResetTTL: time.Hour, PendingRegistrationTTL: 30 * time.Minute, OTPTTL: 5 * time.Minute, OTPResendCooldown: 60 * time.Second},
	})
	return h
}

// registerActive seeds a sign-in-capable account directly. The registration
// flow under test is StartRegistration/VerifyRegistrationOTP; the sign-in,
// session and password tests only need an active account to work against, so
// they seed one rather than driving the whole two-step flow.
func (h *harness) registerActive(email, password string) {
	hash, _ := testHasher{}.Hash(password)
	_ = h.users.Create(context.Background(), &model.Account{
		ID:           uuid.New(),
		Email:        model.NormalizeEmail(email),
		PasswordHash: hash,
		Role:         access.RoleCustomer,
		Status:       constant.StatusActive,
	})
}
