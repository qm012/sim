// Copyright (c) 2026 The Sim Authors
// Use of this source code is governed by a MIT license
// that can be found in the LICENSE file.

package sim

import (
	"context"
	"log/slog"
	"net/http"
	"net/netip"
	"slices"
	"strings"
)

// TrustAllCIDRs trusts every peer; assign to TrustedCIDRs only when a
// trusted proxy always overwrites the forwarding headers.
var TrustAllCIDRs = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/0"),
	netip.MustParsePrefix("::/0"),
}

// ClientIPResolution resolves the client IP address and stores it in the request
// context, where [RequestLogging] and [ClientIPFromContext] read it.
//
// The remote address is always reported. The X-Forwarded-For and X-Real-IP
// headers are consulted only when the peer is inside [ClientIPResolution.TrustedCIDRs];
// a header from any other peer is ignored, since anyone can set it.
// For a trusted peer, the X-Forwarded-For chain is walked from the right,
// skipping trusted proxies, so an attacker cannot forge an entry past the
// last trusted hop.
type ClientIPResolution struct {
	// TrustedCIDRs lists the peer CIDRs whose X-Forwarded-For and
	// X-Real-IP headers are trusted. Nil or empty trusts no peer: only
	// the remote address is reported. Peers are compared after folding
	// IPv4-mapped IPv6 to plain IPv4, so an IPv6-only prefix such as
	// "::/0" never matches IPv4 peers; to trust every peer, assign
	// [TrustAllCIDRs]. Typical values are your reverse proxy's CIDRs,
	// e.g. netip.MustParsePrefix("10.0.0.0/8") or
	// netip.MustParsePrefix("2001:db8::/32").
	TrustedCIDRs []netip.Prefix

	// Lookup specifies an optional function consulted before the built-in
	// resolution. A non-empty result is used as the client IP as-is,
	// bypassing the trust gate; an empty result falls back to the built-in
	// resolution. It can trust headers the built-in resolution does not
	// read, such as CF-Connecting-IP, but callers must ensure their
	// deployment overwrites the header, or a client can forge the reported
	// IP.
	Lookup func(*http.Request) string
}

// NewClientIPResolution returns a new ClientIPResolution.
func NewClientIPResolution() *ClientIPResolution {
	return &ClientIPResolution{}
}

// Handler resolves the client IP for each request and stores it in the
// request context for [ClientIPFromContext].
// It captures the current field values at call time;
// later changes do not affect the returned handler.
func (c *ClientIPResolution) Handler(h http.Handler) http.Handler {
	// Snapshot the configuration; c is not referenced after this point.
	trustedCIDRs := c.TrustedCIDRs
	lookup := c.Lookup
	if slices.Equal(trustedCIDRs, TrustAllCIDRs) {
		slog.Warn("client ip resolution trusts all peers: any client can forge X-Forwarded-For and X-Real-IP",
			slog.String("hint", "only use TrustAllCIDRs when your front-end proxy reliably overwrites "+
				"forwarding headers; prefer assigning your proxy CIDRs to ClientIPResolution.TrustedCIDRs, "+
				`e.g. []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}`))
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := ""
		if lookup != nil {
			ip = lookup(r)
		}
		if ip == "" {
			ip = clientIP(r, trustedCIDRs)
		}
		ctx := context.WithValue(r.Context(), ctxClientIPKey{}, ip)
		h.ServeHTTP(w, r.WithContext(ctx))
	})
}

// ClientIPFromContext returns the client IP stored by [ClientIPResolution.Handler],
// or "" when the request was not wrapped.
func ClientIPFromContext(ctx context.Context) string {
	if ip, ok := ctx.Value(ctxClientIPKey{}).(string); ok {
		return ip
	}
	return ""
}

type ctxClientIPKey struct{}

// clientIP returns the client IP for r, or "" when it cannot be determined.
func clientIP(r *http.Request, trustedCIDRs []netip.Prefix) string {
	peerIPAddr := peerIP(r)
	if !peerIPAddr.IsValid() {
		return ""
	}

	if slices.ContainsFunc(trustedCIDRs, func(cidr netip.Prefix) bool {
		return cidr.Contains(peerIPAddr)
	}) {
		if ip := ipFromXForwardedFor(r.Header, trustedCIDRs); ip.IsValid() {
			return ip.String()
		}
		// X-Real-IP carries a single value: the trusted proxy's verdict.
		if ip, err := netip.ParseAddr(strings.TrimSpace(r.Header.Get("X-Real-IP"))); err == nil {
			return ip.Unmap().String()
		}
	}
	return peerIPAddr.String()
}

// ipFromXForwardedFor walks the X-Forwarded-For chain from the right,
// skipping trusted proxies, and returns the first entry that is not a
// trusted proxy, or the leftmost entry when the whole chain is trusted.
// It returns the zero Addr when the chain is absent or malformed.
func ipFromXForwardedFor(header http.Header, trustedCIDRs []netip.Prefix) netip.Addr {
	value := strings.Join(header.Values("X-Forwarded-For"), ",")
	for i, entry := range slices.Backward(strings.Split(value, ",")) {
		ip, err := netip.ParseAddr(strings.TrimSpace(entry))
		if err != nil {
			return netip.Addr{}
		}
		ip = ip.Unmap()
		// i is the original left-to-right index even though the walk is
		// backward; i == 0 is the leftmost entry, returned even if it too
		// falls within a trusted CIDR (there is nothing further left to skip to).
		if i == 0 || !slices.ContainsFunc(trustedCIDRs, func(cidr netip.Prefix) bool {
			return cidr.Contains(ip)
		}) {
			return ip
		}
	}
	return netip.Addr{}
}

// peerIP returns the host of r.RemoteAddr, folding IPv4-mapped IPv6
// (::ffff:a.b.c.d) to plain IPv4, or the zero Addr when it is not in
// host:port form.
func peerIP(r *http.Request) netip.Addr {
	addrPort, err := netip.ParseAddrPort(strings.TrimSpace(r.RemoteAddr))
	if err != nil {
		return netip.Addr{}
	}
	return addrPort.Addr().Unmap()
}
