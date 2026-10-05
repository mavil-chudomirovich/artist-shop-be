package middleware

import (
	"net"
	"net/http"
	"strings"
)

// RealIP resolves the caller IP from X-Forwarded-For, but only when the direct
// peer is a trusted proxy. Without an allowlist the header is ignored, so a
// client cannot spoof its IP to bypass per-IP rate limits or login lockout.
func RealIP(trustedProxies []string) func(http.Handler) http.Handler {
	nets := parseCIDRs(trustedProxies)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if peer, _, err := net.SplitHostPort(r.RemoteAddr); err == nil && trusted(nets, peer) {
				if forwarded := clientFromXFF(r.Header.Get("X-Forwarded-For")); forwarded != "" {
					r.RemoteAddr = net.JoinHostPort(forwarded, "0")
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// parseCIDRs converts entries to networks; plain IPs become single-host masks.
// Unparsable entries are skipped so a typo cannot trust every proxy.
func parseCIDRs(entries []string) []*net.IPNet {
	nets := make([]*net.IPNet, 0, len(entries))
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if _, network, err := net.ParseCIDR(entry); err == nil {
			nets = append(nets, network)
			continue
		}
		if ip := net.ParseIP(entry); ip != nil {
			bits := 128
			if ip.To4() != nil {
				ip = ip.To4()
				bits = 32
			}
			nets = append(nets, &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)})
		}
	}
	return nets
}

func trusted(nets []*net.IPNet, peer string) bool {
	ip := net.ParseIP(peer)
	if ip == nil {
		return false
	}
	for _, network := range nets {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

// clientFromXFF returns the left-most address in the X-Forwarded-For chain.
func clientFromXFF(header string) string {
	if header == "" {
		return ""
	}
	first, _, _ := strings.Cut(header, ",")
	first = strings.TrimSpace(first)
	if net.ParseIP(first) == nil {
		return ""
	}
	return first
}
