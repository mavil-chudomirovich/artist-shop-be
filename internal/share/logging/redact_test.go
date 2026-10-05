package logging

import (
	"bytes"
	"context"
	"encoding/base64"
	"log/slog"
	"net/url"
	"strings"
	"testing"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/reqctx"
)

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
