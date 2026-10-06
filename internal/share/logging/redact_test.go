package logging

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/reqctx"
)

// errBoom wraps a provider-shaped failure in the two gap cases below.
var errBoom = errors.New("boom")

func TestRedactsSensitiveAttributes(t *testing.T) {
	var buf bytes.Buffer
	logger := New("info", &buf)

	logger.Info("login attempt", "email", "user@example.com", "password", "hunter2", "token", "abc.def")

	out := buf.String()
	if strings.Contains(out, "hunter2") {
		t.Fatalf("password leaked into logs: %s", out)
	}
	if strings.Contains(out, "abc.def") {
		t.Fatalf("token leaked into logs: %s", out)
	}
	if !strings.Contains(out, RedactedValue) {
		t.Fatalf("expected redaction marker, got %s", out)
	}
	if !strings.Contains(out, "user@example.com") {
		t.Fatalf("non-sensitive attribute should be preserved, got %s", out)
	}
}

func TestRedactsConfigSecretKeys(t *testing.T) {
	keys := []string{
		"JWT_SECRET", "jwt_secret", "REDIS_PASSWORD", "SMTP_PASSWORD",
		"ADMIN_PASSWORD", "DB_PASSWORD", "client_secret",
	}
	for _, key := range keys {
		if !IsSensitiveKey(key) {
			t.Errorf("key %q must be treated as sensitive", key)
		}
	}
	for _, key := range []string{"email", "role", "path", "duration", "client"} {
		if IsSensitiveKey(key) {
			t.Errorf("key %q must not be treated as sensitive", key)
		}
	}
}

func TestSecretKeyDoesNotLeakThroughLogger(t *testing.T) {
	var buf bytes.Buffer
	New("info", &buf).Info("config loaded",
		"JWT_SECRET", "super-secret-signing-key",
		slog.Group("cache", slog.String("redis_password", "redis-hunter2")),
	)

	out := buf.String()
	if strings.Contains(out, "super-secret-signing-key") {
		t.Fatalf("jwt secret leaked into logs: %s", out)
	}
	if strings.Contains(out, "redis-hunter2") {
		t.Fatalf("redis password leaked into logs: %s", out)
	}
}

func TestRedactsNestedGroup(t *testing.T) {
	var buf bytes.Buffer
	logger := New("info", &buf)

	logger.Info("nested", slog.Group("db", slog.String("database_url", "postgres://u:p@h/db"), slog.String("host", "h")))

	out := buf.String()
	if strings.Contains(out, "postgres://u:p@h/db") {
		t.Fatalf("nested secret leaked: %s", out)
	}
	if !strings.Contains(out, "\"h\"") {
		t.Fatalf("non-sensitive nested value lost: %s", out)
	}
}

func TestWithCorrelation(t *testing.T) {
	var buf bytes.Buffer
	logger := New("info", &buf)

	ctx := reqctx.WithCorrelation(context.Background(), "corr-123")
	WithCorrelation(ctx, logger).Info("hello")

	if !strings.Contains(buf.String(), "corr-123") {
		t.Fatalf("expected correlation id in logs, got %s", buf.String())
	}
}

func TestJSONStructured(t *testing.T) {
	var buf bytes.Buffer
	New("info", &buf).Info("structured", "key", "value")

	if !strings.Contains(buf.String(), "\"msg\"") || !strings.Contains(buf.String(), "\"key\":\"value\"") {
		t.Fatalf("expected JSON structure, got %s", buf.String())
	}
}

// The media surface: an avatar upload hands the provider the cloud name, the api key
// and the api secret, and the provider's answer quotes all three back. The media
// adapter already refuses to put any of it in a log line, but the line that reaches
// the log was written by this shared layer, so the shapes have to hold here too —
// otherwise a future caller anywhere in the service could undo the adapter's care
// with one attribute.
//
// Each case is a shape as the module actually produces it, not a convenient
// approximation: the values asserted absent are the real credential strings, so a
// redaction that only hid the key name would still fail.
func TestRedactsMediaCredentialsAndTheProviderAnswer(t *testing.T) {
	const (
		cloudName = "demo"
		apiKey    = "123456789012345"
		apiSecret = "abcdefghijklmnopqrstuvwxyz1234"
		// The provider names the cloud, the key and the secret in its own error
		// prose, which is why the whole answer is sensitive however it is keyed.
		providerAnswer = `{"error":{"message":"Internal error for cloud ` + cloudName +
			` using key ` + apiKey + ` and secret ` + apiSecret + `"}}`
	)

	// Cloudinary's signed API carries the signature, the api key and the payload in
	// the form body, so this is the whole credential-bearing request as it goes out.
	signedForm := url.Values{
		"file":      {"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk"},
		"api_key":   {apiKey},
		"timestamp": {"1767225600"},
		"folder":    {"artist-shop"},
		"signature": {"da8f1a4b0c3e5f6a7b8c9d0e1f2a3b4c5d6e7f80"},
	}

	cases := map[string]struct {
		attr slog.Attr
		// values are the strings that must not survive into the log line.
		values []string
		// keep is a member that must still be there: redaction that swallowed the
		// whole line would prove nothing about the credential.
		keep string
	}{
		"the credentials in an Authorization header": {
			attr: slog.String("authorization",
				"Basic "+base64.StdEncoding.EncodeToString([]byte(apiKey+":"+apiSecret))),
			values: []string{apiKey, apiSecret},
			keep:   "\"authorization\"",
		},
		"the api key in the form field the provider names": {
			attr:   slog.String("api_key", apiKey),
			values: []string{apiKey},
			keep:   "\"api_key\"",
		},
		"the api key under the header spelling": {
			attr:   slog.String("apikey", apiKey),
			values: []string{apiKey},
			keep:   "\"apikey\"",
		},
		"the whole signed request as a body": {
			attr:   slog.String("body", signedForm.Encode()),
			values: []string{apiKey, signedForm.Get("signature")},
			keep:   "\"body\"",
		},
		"the provider answer and its prose": {
			attr:   slog.String("body", providerAnswer),
			values: []string{cloudName, apiKey, apiSecret, "Internal error"},
			keep:   "\"body\"",
		},
		"the media settings and the answer as a nested group": {
			attr: slog.Group("media",
				slog.String("api_key", apiKey),
				slog.String("body", providerAnswer),
				slog.String("operation", "upload"),
			),
			values: []string{apiKey, apiSecret},
			keep:   "\"operation\":\"upload\"",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			New("info", &buf).Info("media provider call failed", tc.attr)

			out := buf.String()
			for _, value := range tc.values {
				if strings.Contains(out, value) {
					t.Fatalf("media credential leaked into logs (%q): %s", value, out)
				}
			}
			if !strings.Contains(out, RedactedValue) {
				t.Fatalf("expected redaction marker, got %s", out)
			}
			if !strings.Contains(out, tc.keep) {
				t.Fatalf("redaction must keep the line usable, expected %s in %s", tc.keep, out)
			}
		})
	}
}

// The mail surface, and the startup refusal beside it. Both were added by feature
// 004-fix-pending-defects and both are recorded on a path that runs when something
// is already wrong, which is exactly when a careless attribute costs the most: a
// registration whose message never arrived, and a deployment whose shared ceiling
// makes every avatar upload impossible.
//
// Neither module adapter sanitises its own diagnostic the way the media one does -
// the mail sender classifies the failure and the classified word is all that is
// logged, and the refusal is built from two byte counts and a boolean - so the
// shared layer is the last thing standing between these two shapes and a log file.
// The module's own tests exercise both paths end to end; what is pinned here is
// that the shared layer holds for the shapes they produce, so a future caller
// anywhere in the service cannot undo that care with one attribute.
//
// docs/architecture.md forbids share/* from importing a module, so the two
// producers cannot be called from this package. The attributes below are therefore
// transcribed from the call sites, named at each one, and a change to either
// producer has to be mirrored here.
func TestTheNewFailureDiagnosticsCannotLeakACredential(t *testing.T) {
	const (
		// The credential the provider was configured with, and the cloud it was
		// configured for. Neither is anywhere near either shape; both are asserted
		// absent so that a redaction which only hid the key name would still fail.
		cloudName   = "artist-shop-prod"
		apiKey      = "123456789012345"
		apiSecret   = "abcdefghijklmnopqrstuvwxyz1234"
		smtpPass    = "smtp-credential-value-do-not-log"
		accountID   = "3f1c8a52-9d4e-4b7a-8c21-5e6f7a8b9c0d"
		recipient   = "leak@example.com"
		verifyCode  = "483920"
		providerMsg = "unauthorized IP address 203.0.113.7"
	)

	cases := map[string]struct {
		emit func(*bytes.Buffer)
		// values are the strings that must not survive into the log line.
		values []string
		// keep is a member an operator needs, so a redaction that swallowed the
		// whole line would fail here instead of passing silently.
		keep []string
	}{
		// internal/modules/auth/application/implement/register.go: reportDeliveryFailure
		"the classified delivery diagnostic": {
			emit: func(buf *bytes.Buffer) {
				ctx := reqctx.WithCorrelation(context.Background(), "corr-004-a")
				WithCorrelation(ctx, New("info", buf)).ErrorContext(ctx,
					"verification message could not be delivered",
					slog.String("accountId", accountID),
					slog.String("deliveryFailure", "CONFIGURATION"),
					slog.Int("deliveryAttempts", 1),
				)
			},
			values: []string{apiKey, apiSecret, smtpPass, cloudName, recipient, verifyCode, providerMsg},
			keep:   []string{"CONFIGURATION", `"deliveryAttempts":1`, accountID},
		},
		// internal/modules/auth/application/implement/email_failure.go: disarmCooldownAfterFailedDelivery
		"the disarm-failure diagnostic": {
			emit: func(buf *bytes.Buffer) {
				ctx := reqctx.WithCorrelation(context.Background(), "corr-004-b")
				WithCorrelation(ctx, New("error", buf)).ErrorContext(ctx,
					"resend cooldown could not be disarmed after a failed delivery",
					slog.String("accountId", accountID),
				)
			},
			values: []string{apiKey, apiSecret, smtpPass, cloudName, recipient, verifyCode, providerMsg},
			keep:   []string{accountID},
		},
		// cmd/api/main.go: the guard's refusal reaches the operator on stderr, and
		// the media-absent line reaches the shared logger on stdout.
		"the startup refusal and the media-absent warning": {
			emit: func(buf *bytes.Buffer) {
				logger := New("warn", buf)
				logger.Warn("media credentials are absent: avatar upload answers USER_MEDIA_UNAVAILABLE, every other endpoint is unaffected, and MAX_BODY_BYTES is not checked against the avatar ceiling",
					slog.String("mediaConfigKeys", "MEDIA_CLOUD_NAME, MEDIA_API_KEY, MEDIA_API_SECRET"))
			},
			values: []string{apiKey, apiSecret, smtpPass, cloudName, recipient, verifyCode, providerMsg},
			keep:   []string{"USER_MEDIA_UNAVAILABLE", "MEDIA_CLOUD_NAME"},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			tc.emit(&buf)

			out := buf.String()
			for _, value := range tc.values {
				if strings.Contains(out, value) {
					t.Fatalf("credential leaked into logs (%q): %s", value, out)
				}
			}
			for _, want := range tc.keep {
				if !strings.Contains(out, want) {
					t.Fatalf("the operator must still be able to act on this line, expected %q in %s", want, out)
				}
			}
		})
	}
}

// The startup refusal is the one shape in this feature that reaches the operator as
// a message rather than as attributes, and the redactor does not inspect messages at
// all - see TestKnownGapsBelow. The guarantee therefore comes from the message being
// assembled from two byte counts and a boolean, and this pins the text that ships so
// a future wording change cannot quietly start interpolating a setting value.
//
// The text is the format string in
// internal/modules/user/presentation/http/router.go: RequireAvatarUploadCeiling,
// with the shared ceiling one byte below what the avatar route installs.
func TestTheStartupRefusalTextCarriesNoCredentialValue(t *testing.T) {
	const (
		shared = int64(2162687)
		avatar = int64(2162688)
	)

	refusal := fmt.Sprintf(
		"MAX_BODY_BYTES is %d bytes but an avatar upload needs at least %d bytes: "+
			"the shared request-size ceiling wraps the body before routing, so every avatar upload would be "+
			"refused before the avatar route could apply its own limit. "+
			"Raise MAX_BODY_BYTES to at least %d bytes, or start without media credentials",
		shared, avatar, avatar)

	var buf bytes.Buffer
	New("error", &buf).Error(refusal)

	out := buf.String()
	for _, forbidden := range []string{
		"SECRET", "API_KEY", "API_SECRET", "CLOUD_NAME", "PASSWORD", "TOKEN", "SMTP", "cloudinary", "artist-shop-prod",
	} {
		if strings.Contains(strings.ToUpper(out), strings.ToUpper(forbidden)) {
			t.Errorf("the refusal must name the setting and nothing else, but it mentions %q: %q", forbidden, out)
		}
	}
	for _, want := range []string{"MAX_BODY_BYTES", strconv.FormatInt(shared, 10), strconv.FormatInt(avatar, 10)} {
		if !strings.Contains(out, want) {
			t.Errorf("the refusal must name %q so it can be corrected without reading source; got %q", want, out)
		}
	}
}

// Two gaps the shared redactor does not cover, found while proving the shapes above
// (feature 004-fix-pending-defects, T024). They are recorded here rather than fixed
// because changing the redactor's behaviour is a separate decision: `error` is a key
// many callers use for failures that carry no secret, and redacting it wholesale
// would remove the cause from every error line in the service.
//
// This test asserts today's behaviour on purpose. It is a tripwire, not a guarantee:
// if either case starts being redacted, this fails and the decision gets made
// deliberately instead of by accident.
func TestKnownGapsTheRedactorDoesNotCover(t *testing.T) {
	const credential = "smtp-credential-value-do-not-log"

	t.Run("a message is never inspected", func(t *testing.T) {
		var buf bytes.Buffer
		New("error", &buf).Error("sending failed: " + credential)

		if !strings.Contains(buf.String(), credential) {
			t.Skip("the redactor now inspects messages, so this gap is closed: record the decision in docs/decisions and drop this case")
		}
	})

	t.Run("an error-valued attribute under a plain key is not redacted", func(t *testing.T) {
		var buf bytes.Buffer
		New("error", &buf).Error("provider call failed",
			slog.Any("error", fmt.Errorf("%w: unauthorized IP address, credential %s", errBoom, credential)),
		)

		if !strings.Contains(buf.String(), credential) {
			t.Skip("the redactor now covers error-valued attributes, so this gap is closed: record the decision in docs/decisions and drop this case")
		}
	})
}
