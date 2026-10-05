package media

import (
	"bytes"
	"context"
	"crypto/sha1" //nolint:gosec // The provider mandates SHA-1; this recomputes its signature independently.
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"testing"

	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/application/interface"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/config"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/reqctx"
)

// appReference builds the port's reference value, so a test never has to spell the
// struct out and an unset reference reads as one expression.
func appReference(publicID, link string, width, height int) appinterface.MediaReference {
	return appinterface.MediaReference{PublicID: publicID, URL: link, Width: width, Height: height}
}

const (
	testCloud  = "demo"
	testKey    = "123456789012345"
	testSecret = "abcdefghijklmnopqrstuvwxyz1234"
)

// capturedRequest is what a fake provider saw.
type capturedRequest struct {
	path string
	form url.Values
}

// fakeProvider is an httptest server standing in for Cloudinary. It records the
// request it received and answers whatever the test told it to, so the adapter's
// own signature computation can be checked against an independent one.
type fakeProvider struct {
	server *httptest.Server
	seen   []capturedRequest
	// status and body are the answer given to the next call.
	status int
	body   string
}

func newFakeProvider(t *testing.T) *fakeProvider {
	t.Helper()
	provider := &fakeProvider{
		status: http.StatusOK,
		body: `{"public_id":"artist-shop/avatars/one",` +
			`"secure_url":"https://res.cloudinary.com/demo/image/upload/artist-shop/avatars/one.jpg",` +
			`"width":512,"height":512}`,
	}
	provider.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse the provider request form: %v", err)
		}
		provider.seen = append(provider.seen, capturedRequest{path: r.URL.Path, form: r.Form})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(provider.status)
		if _, err := w.Write([]byte(provider.body)); err != nil {
			t.Errorf("write the fake provider answer: %v", err)
		}
	}))
	t.Cleanup(provider.server.Close)
	return provider
}

func (p *fakeProvider) last(t *testing.T) capturedRequest {
	t.Helper()
	if len(p.seen) == 0 {
		t.Fatal("the adapter never called the provider")
	}
	return p.seen[len(p.seen)-1]
}

func testConfig() config.MediaConfig {
	return config.MediaConfig{
		CloudName: testCloud,
		APIKey:    testKey,
		APISecret: testSecret,
		Folder:    "artist-shop",
	}
}

// testUnsignedParameters mirrors the exclusion list the provider documents. Keeping it
// here rather than reusing the adapter's own copy is deliberate: the test has to
// state the rule independently, or a change on both sides at once would still pass.
var testUnsignedParameters = map[string]bool{
	"file":          true,
	"cloud_name":    true,
	"resource_type": true,
	"api_key":       true,
	"signature":     true,
}

// expectedSignature recomputes Cloudinary's signature from the specification,
// independently of the adapter: the signed parameters sorted by key, joined as
// "key=value" with "&", the API secret appended, SHA-1, lowercase hex.
func expectedSignature(form url.Values) string {
	keys := make([]string, 0, len(form))
	for key := range form {
		if testUnsignedParameters[key] {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var payload strings.Builder
	for i, key := range keys {
		if i > 0 {
			payload.WriteByte('&')
		}
		payload.WriteString(key + "=" + form.Get(key))
	}
	payload.WriteString(testSecret)

	sum := sha1.Sum([]byte(payload.String()))
	return hex.EncodeToString(sum[:])
}

func newTestStore(t *testing.T, provider *fakeProvider) *Cloudinary {
	t.Helper()
	return New(testConfig(), provider.server.Client(), discardLogger(), provider.server.URL)
}

// discardLogger keeps the adapter's own log lines out of the test output, for tests
// that are not about logging.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// capturedLogger returns a logger and the buffer its output lands in, so a test can
// read back exactly what the adapter wrote.
func capturedLogger() (*slog.Logger, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	handler := slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	return slog.New(handler), buf
}

// pngBytes is a payload whose base64 form is recognisable in the request.
var pngBytes = append([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}, 0x2A, 0x2A)

// The signature must be the one the API specifies, and the parameters it covers
// must be exactly the ones sent â€” a signature over a different set, or in a
// different order, would be rejected by the provider with an opaque 401.
func TestUploadSignsTheRequestAsTheProviderSpecifies(t *testing.T) {
	provider := newFakeProvider(t)
	store := newTestStore(t, provider)

	if _, err := store.Upload(context.Background(), pngBytes, 512); err != nil {
		t.Fatalf("Upload: %v", err)
	}

	sent := provider.last(t)
	if sent.path != "/v1_1/demo/image/upload" {
		t.Fatalf("expected the signed upload endpoint, got %q", sent.path)
	}
	if sent.form.Get("api_key") != testKey {
		t.Fatalf("expected the api key to travel with the request, got %q", sent.form.Get("api_key"))
	}
	if got := sent.form.Get("signature"); got != expectedSignature(sent.form) {
		t.Fatalf("expected the signature the API specifies\n got %s\nwant %s",
			got, expectedSignature(sent.form))
	}
	if sent.form.Get("signature") == "" {
		t.Fatal("expected a signature on the request")
	}
	if sent.form.Get("timestamp") == "" {
		t.Fatal("expected a timestamp, which is a signed parameter")
	}
}

// ADR-005 and research D6: the original bytes go up unchanged and the resize is
// asked of the provider. The transformation must be a ceiling rather than a forced
// size, so a photo narrower than the target is not stretched.
func TestUploadAsksTheProviderForTheResize(t *testing.T) {
	provider := newFakeProvider(t)
	store := newTestStore(t, provider)

	if _, err := store.Upload(context.Background(), pngBytes, 512); err != nil {
		t.Fatalf("Upload: %v", err)
	}

	sent := provider.last(t)
	if got := sent.form.Get("transformation"); !strings.Contains(got, "w_512") {
		t.Fatalf("expected the target width in the transformation, got %q", got)
	}
	if got := sent.form.Get("transformation"); !strings.Contains(got, "c_limit") {
		t.Fatalf("expected a resize ceiling rather than a forced size, got %q", got)
	}
	if sent.form.Get("folder") != "artist-shop" {
		t.Fatalf("expected the configured folder, got %q", sent.form.Get("folder"))
	}
	want := base64.StdEncoding.EncodeToString(pngBytes)
	if got := sent.form.Get("file"); got != want {
		t.Fatalf("expected the original bytes, unmodified, got %q", got)
	}
}

// The four values the database stores come from the provider's own answer, because
// the provider is the system of record for the dimensions it applied (ADR-005).
func TestUploadReadsTheStoredReferenceFromTheAnswer(t *testing.T) {
	provider := newFakeProvider(t)
	provider.body = `{"public_id":"artist-shop/avatars/one",` +
		`"secure_url":"https://res.cloudinary.com/demo/image/upload/artist-shop/avatars/one.jpg",` +
		`"width":512,"height":640}`
	store := newTestStore(t, provider)

	ref, err := store.Upload(context.Background(), pngBytes, 512)

	if err != nil {
		t.Fatalf("Upload: %v", err)
	}
	want := appReference("artist-shop/avatars/one",
		"https://res.cloudinary.com/demo/image/upload/artist-shop/avatars/one.jpg", 512, 640)
	if ref != want {
		t.Fatalf("expected %+v, got %+v", want, ref)
	}
}

// Every provider failure is the module's retryable error, and none of it carries
// the provider's own words: the answer body is a plausible place for a provider to
// name the cloud, the account or an internal detail, and presentation logs
// whatever error it is handed.
func TestProviderFailuresBecomeTheRetryableMediaError(t *testing.T) {
	cases := map[string]struct {
		status int
		body   string
	}{
		"internal server error": {
			status: http.StatusInternalServerError,
			body:   `{"error":{"message":"Internal error, cloud=demo, secret=` + testSecret + `"}}`,
		},
		"unauthorized": {
			status: http.StatusUnauthorized,
			body:   `{"error":{"message":"Invalid signature for cloud demo"}}`,
		},
		"rate limited": {
			status: http.StatusTooManyRequests,
			body:   `{"error":{"message":"Too many requests"}}`,
		},
		"unparsable body": {
			status: http.StatusOK,
			body:   `<html>not json</html>`,
		},
		"success without a reference": {
			status: http.StatusOK,
			body:   `{"resource_type":"image"}`,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			provider := newFakeProvider(t)
			provider.status = tc.status
			provider.body = tc.body
			store := newTestStore(t, provider)

			ref, err := store.Upload(context.Background(), pngBytes, 512)

			if !errors.Is(err, domainerr.ErrMediaUnavailable) {
				t.Fatalf("expected ErrMediaUnavailable, got %v", err)
			}
			if ref != (appReference("", "", 0, 0)) {
				t.Fatalf("expected no reference, got %+v", ref)
			}
			for _, secret := range []string{testSecret, testKey, testCloud, "demo"} {
				if strings.Contains(err.Error(), secret) {
					t.Fatalf("the error leaked %q: %s", secret, err.Error())
				}
			}
			if strings.Contains(err.Error(), "not json") || strings.Contains(err.Error(), "Too many") {
				t.Fatalf("the error carried the provider's own words: %s", err.Error())
			}
		})
	}
}

// A transport failure is the same retryable error: an unreachable provider is an
// outage, not a client mistake.
func TestAnUnreachableProviderBecomesTheRetryableMediaError(t *testing.T) {
	provider := newFakeProvider(t)
	// Closing the server before the call makes the transport fail without
	// depending on how the operating system reports a refused connection.
	url := provider.server.URL
	provider.server.Close()
	store := New(testConfig(), nil, discardLogger(), url)

	ref, err := store.Upload(context.Background(), pngBytes, 512)

	if !errors.Is(err, domainerr.ErrMediaUnavailable) {
		t.Fatalf("expected ErrMediaUnavailable, got %v", err)
	}
	if ref != (appReference("", "", 0, 0)) {
		t.Fatalf("expected no reference, got %+v", ref)
	}
	if strings.Contains(err.Error(), url) || strings.Contains(err.Error(), testCloud) {
		t.Fatalf("the error leaked the endpoint: %s", err.Error())
	}
}

// Releasing a stored reference signs the destroy call the same way, and releasing
// an unknown reference succeeds so a retry never fails on an asset that is already
// gone.
func TestRemoveSignsTheDestroyCallAndTreatsAnUnknownAssetAsDone(t *testing.T) {
	provider := newFakeProvider(t)
	provider.body = `{"result":"ok"}`
	store := newTestStore(t, provider)

	if err := store.Remove(context.Background(), appReference("avatars/one",
		"https://res.cloudinary.com/demo/image/upload/avatars/one.jpg", 512, 512)); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	sent := provider.last(t)
	if sent.path != "/v1_1/demo/image/destroy" {
		t.Fatalf("expected the destroy endpoint, got %q", sent.path)
	}
	if sent.form.Get("public_id") != "avatars/one" {
		t.Fatalf("expected the stored identifier, got %q", sent.form.Get("public_id"))
	}
	if got := sent.form.Get("signature"); got != expectedSignature(sent.form) {
		t.Fatalf("expected the signature the API specifies, got %s want %s", got, expectedSignature(sent.form))
	}

	// The provider answers a destroy of an unknown identifier with 200 and a
	// "not found" result; that is a success, not a failure.
	provider.body = `{"result":"not found"}`
	if err := store.Remove(context.Background(), appReference("avatars/gone", "", 512, 512)); err != nil {
		t.Fatalf("releasing an unknown reference must succeed, got %v", err)
	}

	// A reference that was never stored needs no provider call at all.
	before := len(provider.seen)
	if err := store.Remove(context.Background(), appReference("", "", 0, 0)); err != nil {
		t.Fatalf("removing an empty reference must succeed, got %v", err)
	}
	if len(provider.seen) != before {
		t.Fatal("an empty reference must not call the provider")
	}
}

func TestRemoveReportsAProviderFailureAsTheRetryableMediaError(t *testing.T) {
	provider := newFakeProvider(t)
	provider.status = http.StatusBadGateway
	provider.body = `{"error":{"message":"upstream down"}}`
	store := newTestStore(t, provider)

	err := store.Remove(context.Background(), appReference("avatars/one", "", 512, 512))

	if !errors.Is(err, domainerr.ErrMediaUnavailable) {
		t.Fatalf("expected ErrMediaUnavailable, got %v", err)
	}
	if strings.Contains(err.Error(), "upstream down") {
		t.Fatalf("the provider's words reached the error: %s", err.Error())
	}
}

// The media settings are deliberately outside Validate, so a deployment can reach
// the API with no credentials at all. The adapter then fails closed on every call
// rather than inventing a reference or reporting a success, which is what makes a
// missing configuration disable avatar upload and nothing else â€” the profile read
// never calls the provider (FR-021, FR-017).
func TestMissingCredentialsFailClosed(t *testing.T) {
	cases := map[string]config.MediaConfig{
		"nothing set":        {},
		"no cloud name":      {APIKey: testKey, APISecret: testSecret},
		"no api key":         {CloudName: testCloud, APISecret: testSecret},
		"no api secret":      {CloudName: testCloud, APIKey: testKey},
		"a blank api secret": {CloudName: testCloud, APIKey: testKey, APISecret: ""},
	}

	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			provider := newFakeProvider(t)
			store := New(cfg, provider.server.Client(), discardLogger(), provider.server.URL)

			ref, err := store.Upload(context.Background(), pngBytes, 512)
			if !errors.Is(err, domainerr.ErrMediaUnavailable) {
				t.Fatalf("expected ErrMediaUnavailable, got %v", err)
			}
			if ref != (appReference("", "", 0, 0)) {
				t.Fatalf("expected no reference, got %+v", ref)
			}
			if err := store.Remove(context.Background(), appReference("avatars/one", "", 512, 512)); !errors.Is(err, domainerr.ErrMediaUnavailable) {
				t.Fatalf("expected ErrMediaUnavailable from Remove, got %v", err)
			}
			// No request may reach a provider that was never configured, and the
			// error may not carry any of the partial configuration.
			if len(provider.seen) != 0 {
				t.Fatalf("an unconfigured provider must not be called, got %d calls", len(provider.seen))
			}
			for _, value := range []string{cfg.CloudName, cfg.APIKey, cfg.APISecret} {
				if value != "" && strings.Contains(err.Error(), value) {
					t.Fatalf("the error leaked a credential: %s", err.Error())
				}
			}
		})
	}
}

// The adapter is constructed against the real API root in production, and the root
// is only what the test overrides.
func TestTheDefaultEndpointIsCloudinarys(t *testing.T) {
	store := NewWithDefaults(testConfig(), discardLogger())
	if store.baseURL != "https://api.cloudinary.com" {
		t.Fatalf("expected Cloudinary's API root, got %q", store.baseURL)
	}
	if store.client.Timeout <= 0 {
		t.Fatal("expected the default client to carry a bounded timeout")
	}
	if store.uploadPath == store.destroyPath {
		t.Fatal("upload and destroy must be different endpoints")
	}
}

// A flattened error leaves an operator with nothing at all to look at, so the
// adapter logs its own sanitised classification of the failure. The level is part of
// the contract: a refusal is a warning because the provider answered and a retry or
// a configuration change resolves it, while an answer this service cannot read is an
// error because nothing about it is actionable.
//
// The other half of the contract is what the line must never carry. The fake
// provider is configured to answer with a body that names the cloud, the api key and
// the secret, so a leak would show up here rather than being argued about.
func TestProviderFailuresAreLoggedWithoutLeakingAnything(t *testing.T) {
	const correlationID = "corr-000000000042"

	cases := map[string]struct {
		status int
		body   string
		want   string
		level  string
	}{
		"refused request": {
			status: http.StatusInternalServerError,
			body:   `{"error":{"message":"Internal error for cloud ` + testCloud + ` using key ` + testKey + ` and secret ` + testSecret + `"}}`,
			want:   "the provider refused the request (status 500)",
			level:  slog.LevelWarn.String(),
		},
		"unreadable answer": {
			status: http.StatusOK,
			body:   `<html>not json, cloud=` + testCloud + ` secret=` + testSecret + `</html>`,
			want:   "the provider answer could not be read (status 200)",
			level:  slog.LevelError.String(),
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			provider := newFakeProvider(t)
			provider.status = tc.status
			provider.body = tc.body
			logger, output := capturedLogger()
			store := New(testConfig(), provider.server.Client(), logger, provider.server.URL)

			ctx := reqctx.WithCorrelation(context.Background(), correlationID)
			if _, err := store.Upload(ctx, pngBytes, 512); !errors.Is(err, domainerr.ErrMediaUnavailable) {
				t.Fatalf("expected ErrMediaUnavailable, got %v", err)
			}

			logged := output.String()
			if logged == "" {
				t.Fatal("a provider failure left no log line to diagnose it with")
			}
			if !strings.Contains(logged, tc.want) {
				t.Fatalf("expected the classification %q in the log, got:\n%s", tc.want, logged)
			}
			if !strings.Contains(logged, "level="+tc.level) {
				t.Fatalf("expected the failure logged at %s, got:\n%s", tc.level, logged)
			}
			if !strings.Contains(logged, correlationID) {
				t.Fatalf("expected the correlation id in the log, got:\n%s", logged)
			}
			if !strings.Contains(logged, "operation=upload") {
				t.Fatalf("expected the failing operation in the log, got:\n%s", logged)
			}
			forbidden := map[string]string{
				"the api secret":     testSecret,
				"the api key":        testKey,
				"the cloud name":     testCloud,
				"the provider prose": "Internal error for cloud",
				"the upload url":     provider.server.URL,
				"the uploaded bytes": base64.StdEncoding.EncodeToString(pngBytes),
			}
			for label, value := range forbidden {
				if strings.Contains(logged, value) {
					t.Fatalf("the log line leaked %s (%q):\n%s", label, value, logged)
				}
			}
		})
	}
}

// The same sanitised classification reaches both the operator and the caller, so a
// failed upload is diagnosable from the log and the error still carries nothing the
// log must hide.
func TestTheLoggedClassificationIsTheOneTheCallerGets(t *testing.T) {
	provider := newFakeProvider(t)
	provider.status = http.StatusBadGateway
	provider.body = `{"error":{"message":"upstream down"}}`
	logger, output := capturedLogger()
	store := New(testConfig(), provider.server.Client(), logger, provider.server.URL)

	_, err := store.Upload(context.Background(), pngBytes, 512)

	if err == nil {
		t.Fatal("expected a provider failure")
	}
	classification := strings.TrimPrefix(err.Error(), domainerr.ErrMediaUnavailable.Error()+": ")
	if !strings.Contains(output.String(), classification) {
		t.Fatalf("the logged classification %q differs from the returned one:\n%s",
			classification, output.String())
	}
}

// A nil logger must not turn a provider outage into a panic: the failure path is
// exactly when a crash is least welcome.
func TestANilLoggerStillFailsCleanly(t *testing.T) {
	// The fallback is the default logger, which would otherwise write the
	// classification into the test output.
	previous := slog.Default()
	slog.SetDefault(discardLogger())
	t.Cleanup(func() { slog.SetDefault(previous) })

	provider := newFakeProvider(t)
	provider.status = http.StatusInternalServerError
	store := New(testConfig(), provider.server.Client(), nil, provider.server.URL)

	if _, err := store.Upload(context.Background(), pngBytes, 512); !errors.Is(err, domainerr.ErrMediaUnavailable) {
		t.Fatalf("expected ErrMediaUnavailable, got %v", err)
	}
}
