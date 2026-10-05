package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func remoteAddrOf(t *testing.T, trustedProxies []string, remoteAddr, xff string) string {
	t.Helper()
	var got string
	handler := RealIP(trustedProxies)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = r.RemoteAddr
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = remoteAddr
	if xff != "" {
		req.Header.Set("X-Forwarded-For", xff)
	}
	handler.ServeHTTP(httptest.NewRecorder(), req)
	return got
}

func TestRealIPIgnoresHeaderFromUntrustedPeer(t *testing.T) {
	got := remoteAddrOf(t, []string{"10.0.0.0/8"}, "203.0.113.9:5555", "1.2.3.4")
	if got != "203.0.113.9:5555" {
		t.Fatalf("expected untrusted peer preserved, got %q", got)
	}
}

func TestRealIPUsesHeaderFromTrustedProxy(t *testing.T) {
	got := remoteAddrOf(t, []string{"10.0.0.0/8"}, "10.0.0.5:5555", "1.2.3.4, 10.0.0.5")
	if got != "1.2.3.4:0" {
		t.Fatalf("expected client IP, got %q", got)
	}
}

func TestRealIPWithoutAllowlistIgnoresHeader(t *testing.T) {
	got := remoteAddrOf(t, nil, "10.0.0.5:5555", "1.2.3.4")
	if got != "10.0.0.5:5555" {
		t.Fatalf("expected no trust by default, got %q", got)
	}
}

func TestRealIPRejectsMalformedHeader(t *testing.T) {
	got := remoteAddrOf(t, []string{"10.0.0.0/8"}, "10.0.0.5:5555", "not-an-ip")
	if got != "10.0.0.5:5555" {
		t.Fatalf("expected malformed header ignored, got %q", got)
	}
}

func TestRealIPSkipsUnparsableAllowlistEntries(t *testing.T) {
	got := remoteAddrOf(t, []string{"garbage", "10.0.0.5"}, "10.0.0.5:5555", "1.2.3.4")
	if got != "1.2.3.4:0" {
		t.Fatalf("expected single-host entry to match, got %q", got)
	}
	got = remoteAddrOf(t, []string{"garbage"}, "10.0.0.5:5555", "1.2.3.4")
	if got != "10.0.0.5:5555" {
		t.Fatalf("expected unparsable-only allowlist to trust nobody, got %q", got)
	}
}
