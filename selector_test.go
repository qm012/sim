// Copyright (c) 2026 The Sim Authors
// Use of this source code is governed by a MIT license
// that can be found in the LICENSE file.

package sim_test

import (
	"net/http"
	"testing"

	"github.com/qm012/sim"
)

func TestSelector(t *testing.T) {
	tests := []struct {
		name       string
		match      func(*http.Request) bool
		target     string
		wantHeader string // expected X-Wrapped value; "" means the header must be absent
	}{
		{
			name:       "match true applies the wrapper",
			match:      func(*http.Request) bool { return true },
			target:     "/",
			wantHeader: "1",
		},
		{
			name:       "match false bypasses the wrapper",
			match:      func(*http.Request) bool { return false },
			target:     "/",
			wantHeader: "",
		},
		{
			name:       "path predicate matches /admin",
			match:      func(r *http.Request) bool { return r.URL.Path == "/admin" },
			target:     "/admin",
			wantHeader: "1",
		},
		{
			name:       "path predicate bypasses other paths",
			match:      func(r *http.Request) bool { return r.URL.Path == "/admin" },
			target:     "/public",
			wantHeader: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := sim.Selector(wrapHeader("X-Wrapped", "1"), tt.match)(markHandler("h"))
			rec := serve(t, h, http.MethodGet, tt.target)
			if got := rec.Header().Get("X-Wrapped"); got != tt.wantHeader {
				t.Errorf("X-Wrapped = %q, want %q", got, tt.wantHeader)
			}
			if got := rec.Body.String(); got != "h" {
				t.Errorf("body = %q, want %q", got, "h")
			}
		})
	}
}

func TestSelectorAppliesWrapperOnce(t *testing.T) {
	applied := 0
	counting := func(h http.Handler) http.Handler {
		applied++
		return h
	}
	h := sim.Selector(counting, func(*http.Request) bool { return true })(markHandler("h"))
	if applied != 1 {
		t.Fatalf("wrapper applied %d times when building the handler, want 1", applied)
	}
	for range 3 {
		rec := serve(t, h, http.MethodGet, "/")
		if got := rec.Body.String(); got != "h" {
			t.Errorf("body = %q, want %q", got, "h")
		}
	}
	if applied != 1 {
		t.Errorf("wrapper applied %d times after serving requests, want still 1", applied)
	}
}
