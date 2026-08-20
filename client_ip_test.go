// Copyright (c) 2026 The Sim Authors
// Use of this source code is governed by a MIT license
// that can be found in the LICENSE file.

package sim_test

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"

	"github.com/qm012/sim"
)

// newRequest builds a GET request, applying mutate when non-nil.
func newRequest(t *testing.T, mutate func(*http.Request)) *http.Request {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
	if mutate != nil {
		mutate(req)
	}
	return req
}

// serveIP wraps h in c's handler and returns the client IP the inner
// handler reads back from the request context.
func serveIP(t *testing.T, c *sim.ClientIPResolution, mutate func(*http.Request)) string {
	t.Helper()
	var got string
	c.Handler(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = sim.ClientIPFromContext(r.Context())
	})).ServeHTTP(httptest.NewRecorder(), newRequest(t, mutate))
	return got
}

//nolint:funlen // table-driven test
func TestClientIPResolution(t *testing.T) {
	trusted := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}
	trustedV6 := []netip.Prefix{netip.MustParsePrefix("2001:db8::/32")}
	tests := []struct {
		name   string
		trust  []netip.Prefix
		lookup func(*http.Request) string
		mutate func(*http.Request)
		want   string
	}{
		// The default configuration trusts no peer: headers are ignored.
		{"default-ignores-headers", nil, nil, func(r *http.Request) {
			r.RemoteAddr = "10.0.0.1:1234"
			r.Header.Set("X-Forwarded-For", "203.0.113.9")
		}, "10.0.0.1"},
		{"default-remote-addr", nil, nil, func(r *http.Request) {
			r.RemoteAddr = "192.0.2.1:1234"
		}, "192.0.2.1"},
		{"default-remote-addr-ipv6", nil, nil, func(r *http.Request) {
			r.RemoteAddr = "[2001:db8::1]:8080"
		}, "2001:db8::1"},
		{"default-remote-addr-no-port", nil, nil, func(r *http.Request) {
			r.RemoteAddr = "192.0.2.1"
		}, ""},

		{"trusted-x-forwarded-for", trusted, nil, func(r *http.Request) {
			r.RemoteAddr = "10.0.0.1:1234"
			r.Header.Set("X-Forwarded-For", "203.0.113.9")
		}, "203.0.113.9"},
		{"trusted-rightmost-untrusted-wins", trusted, nil, func(r *http.Request) {
			r.RemoteAddr = "10.0.0.1:1234"
			r.Header.Set("X-Forwarded-For", "203.0.113.9, 198.51.100.7")
		}, "198.51.100.7"},
		{"trusted-spoofed-leftmost-ignored", trusted, nil, func(r *http.Request) {
			r.RemoteAddr = "10.0.0.1:1234"
			r.Header.Set("X-Forwarded-For", "198.51.100.7, 203.0.113.9")
		}, "203.0.113.9"},
		{"trusted-hops-skipped-to-leftmost", trusted, nil, func(r *http.Request) {
			r.RemoteAddr = "10.0.0.1:1234"
			r.Header.Set("X-Forwarded-For", "203.0.113.9, 10.9.9.9")
		}, "203.0.113.9"},
		{"trusted-malformed-mid-chain-aborts", trusted, nil, func(r *http.Request) {
			r.RemoteAddr = "10.0.0.1:1234"
			r.Header.Set("X-Forwarded-For", "203.0.113.9, garbage")
		}, "10.0.0.1"},
		{"trusted-x-forwarded-for-beats-x-real-ip", trusted, nil, func(r *http.Request) {
			r.RemoteAddr = "10.0.0.1:1234"
			r.Header.Set("X-Forwarded-For", "203.0.113.9")
			r.Header.Set("X-Real-IP", "203.0.113.11")
		}, "203.0.113.9"},
		{"trusted-x-real-ip-fallback", trusted, nil, func(r *http.Request) {
			r.RemoteAddr = "10.0.0.1:1234"
			r.Header.Set("X-Real-IP", "203.0.113.11")
		}, "203.0.113.11"},
		{"trusted-invalid-xff-uses-x-real-ip", trusted, nil, func(r *http.Request) {
			r.RemoteAddr = "10.0.0.1:1234"
			r.Header.Set("X-Forwarded-For", "not-an-ip")
			r.Header.Set("X-Real-IP", "203.0.113.11")
		}, "203.0.113.11"},
		{"trusted-no-headers", trusted, nil, func(r *http.Request) {
			r.RemoteAddr = "10.0.0.1:1234"
		}, "10.0.0.1"},

		// IPv4-mapped IPv6 folds to plain IPv4 before trust checks.
		{"trusted-v4-mapped-peer", trusted, nil, func(r *http.Request) {
			r.RemoteAddr = "[::ffff:10.0.0.1]:1234"
			r.Header.Set("X-Forwarded-For", "203.0.113.9")
		}, "203.0.113.9"},
		{"trusted-v4-mapped-entry", trusted, nil, func(r *http.Request) {
			r.RemoteAddr = "10.0.0.1:1234"
			r.Header.Set("X-Forwarded-For", "::ffff:203.0.113.9")
		}, "203.0.113.9"},

		{"trusted-v6-peer-and-entry", trustedV6, nil, func(r *http.Request) {
			r.RemoteAddr = "[2001:db8::1]:8080"
			r.Header.Set("X-Forwarded-For", "2600:9000::1")
		}, "2600:9000::1"},

		// Headers from an untrusted peer are ignored even with CIDRs set.
		{"untrusted-peer-ignores-headers", trusted, nil, func(r *http.Request) {
			r.RemoteAddr = "192.0.2.1:1234"
			r.Header.Set("X-Forwarded-For", "203.0.113.9")
		}, "192.0.2.1"},

		// Lookup runs first; a non-empty result bypasses the trust gate.
		{"lookup-beats-remote-addr", nil, func(_ *http.Request) string {
			return "203.0.113.7"
		}, func(r *http.Request) {
			r.RemoteAddr = "192.0.2.1:1234"
		}, "203.0.113.7"},
		{"lookup-ignores-untrusted-peer-headers", nil, func(r *http.Request) string {
			return r.Header.Get("CF-Connecting-IP")
		}, func(r *http.Request) {
			r.RemoteAddr = "192.0.2.1:1234"
			r.Header.Set("CF-Connecting-IP", "203.0.113.7")
		}, "203.0.113.7"},
		{"lookup-beats-x-forwarded-for", trusted, func(_ *http.Request) string {
			return "203.0.113.7"
		}, func(r *http.Request) {
			r.RemoteAddr = "10.0.0.1:1234"
			r.Header.Set("X-Forwarded-For", "203.0.113.9")
		}, "203.0.113.7"},
		{"lookup-empty-falls-back", trusted, func(_ *http.Request) string {
			return ""
		}, func(r *http.Request) {
			r.RemoteAddr = "10.0.0.1:1234"
			r.Header.Set("X-Forwarded-For", "203.0.113.9")
		}, "203.0.113.9"},

		{"trust-all-honors-any-peer", sim.TrustAllCIDRs, nil, func(r *http.Request) {
			r.RemoteAddr = "192.0.2.1:1234"
			r.Header.Set("X-Forwarded-For", "203.0.113.9")
		}, "203.0.113.9"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := sim.NewClientIPResolution()
			c.TrustedCIDRs = tt.trust
			c.Lookup = tt.lookup
			if got := serveIP(t, c, tt.mutate); got != tt.want {
				t.Fatalf("ClientIPResolution() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestClientIPResolutionHandlerCapturesFieldsAtCallTime(t *testing.T) {
	c := sim.NewClientIPResolution()
	c.TrustedCIDRs = []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}
	var got string
	h := c.Handler(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = sim.ClientIPFromContext(r.Context())
	}))
	// Mutations after Handler must not affect the returned handler.
	c.TrustedCIDRs = nil

	req := newRequest(t, func(r *http.Request) {
		r.RemoteAddr = "10.0.0.1:1234"
		r.Header.Set("X-Forwarded-For", "203.0.113.9")
	})
	h.ServeHTTP(httptest.NewRecorder(), req)
	if got != "203.0.113.9" {
		t.Fatalf("ClientIPResolution() = %q, want %q", got, "203.0.113.9")
	}
}

func TestClientIPFromContextUnwrapped(t *testing.T) {
	if got := sim.ClientIPFromContext(t.Context()); got != "" {
		t.Fatalf("ClientIPFromContext() = %q, want empty", got)
	}
}
