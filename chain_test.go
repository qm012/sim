// Copyright (c) 2026 The Sim Authors
// Use of this source code is governed by a MIT license
// that can be found in the LICENSE file.

package sim_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/qm012/sim"
)

func TestChainEmpty(t *testing.T) {
	h := markHandler("h")
	rec := serve(t, sim.Chain()(h), http.MethodGet, "/")
	if got := rec.Body.String(); got != "h" {
		t.Errorf("Chain()(h) body = %q, want %q", got, "h")
	}
}

func TestChainFuncEmpty(t *testing.T) {
	h := markHandlerFunc("func")
	rec := serve(t, sim.ChainFunc()(h), http.MethodGet, "/")
	if got := rec.Body.String(); got != "func" {
		t.Errorf("ChainFunc()(h) body = %q, want %q", got, "func")
	}
}

func assertMatchesManualNesting(t *testing.T, name string, composed, manual http.Handler) {
	t.Helper()
	rec1 := serve(t, composed, http.MethodGet, "/")
	rec2 := serve(t, manual, http.MethodGet, "/")
	if got, want := strings.Join(rec1.Header().Values("X-Order"), ""),
		strings.Join(rec2.Header().Values("X-Order"), ""); got != want || got != "abc" {
		t.Errorf("%s order = %q, manual order = %q, want equal and %q", name, got, want, "abc")
	}
	if got, want := rec1.Body.String(), rec2.Body.String(); got != want {
		t.Errorf("%s body = %q, manual body = %q, want equal", name, got, want)
	}
}

func TestChainMatchesManualNesting(t *testing.T) {
	h := markHandler("xs")
	assertMatchesManualNesting(t,
		"Chain(Recover, Logging, Auth)(h) behaves like Recover(Logging(Auth(h)))",
		sim.Chain(
			wrapHeader("X-Order", "a"),
			wrapHeader("X-Order", "b"),
			wrapHeader("X-Order", "c"),
		)(h),
		wrapHeader("X-Order", "a")(
			wrapHeader("X-Order", "b")(
				wrapHeader("X-Order", "c")(h))))
}

func TestChainFuncMatchesManualNesting(t *testing.T) {
	f := markHandlerFunc("h")
	assertMatchesManualNesting(t,
		"ChainFunc(Recover, Logging, Auth)(f) behaves like Recover(Logging(Auth(f)))",
		sim.ChainFunc(
			wrapHeader("X-Order", "a"),
			wrapHeader("X-Order", "b"),
			wrapHeader("X-Order", "c"),
		)(f),
		wrapHeader("X-Order", "a")(
			wrapHeader("X-Order", "b")(
				wrapHeader("X-Order", "c")(f))))
}

func TestChainDefensiveCopy(t *testing.T) {
	ss := []func(http.Handler) http.Handler{wrapHeader("X-Wrapped", "1")}
	c := sim.Chain(ss...)
	// Mutating the caller's slice after Chain must not change the composition.
	ss[0] = wrapHeader("X-Wrapped", "2")
	rec := serve(t, c(markHandler("h")), http.MethodGet, "/")
	if got := rec.Header().Get("X-Wrapped"); got != "1" {
		t.Errorf("composition changed after source slice mutation: got %q, want %q", got, "1")
	}
}

func TestChainFuncDefensiveCopy(t *testing.T) {
	ss := []func(http.Handler) http.Handler{wrapHeader("X-Wrapped", "1")}
	c := sim.ChainFunc(ss...)
	// Mutating the caller's slice after ChainFunc must not change the composition.
	ss[0] = wrapHeader("X-Wrapped", "2")
	rec := serve(t, c(markHandlerFunc("h")), http.MethodGet, "/")
	if got := rec.Header().Get("X-Wrapped"); got != "1" {
		t.Errorf("composition changed after source slice mutation: got %q, want %q", got, "1")
	}
}
