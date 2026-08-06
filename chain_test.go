// Copyright (c) 2026 qm012<1007661792@qq.com>. All rights reserved.
// Use of this source code is governed by a MIT license
// that can be found in the LICENSE file.

package sim

import (
	"net/http"
	"strings"
	"testing"
)

func TestChainEquivalentToNestedApplication(t *testing.T) {
	h := markHandler("h")
	rec1 := serve(t, Chain(
		wrapHeader("X-Order", "a"),
		wrapHeader("X-Order", "b"),
		wrapHeader("X-Order", "c"),
	)(h), http.MethodGet, "/")
	rec2 := serve(t, wrapHeader("X-Order", "a")(
		wrapHeader("X-Order", "b")(
			wrapHeader("X-Order", "c")(h))),
		http.MethodGet, "/")

	if got, want := strings.Join(rec1.Header().Values("X-Order"), ""),
		strings.Join(rec2.Header().Values("X-Order"), ""); got != want || got != "abc" {
		t.Errorf("Chain(a,b,c)(h) order = %q, a(b(c(h))) order = %q, want equal and %q", got, want, "abc")
	}
	if got, want := rec1.Body.String(), rec2.Body.String(); got != want {
		t.Errorf("Chain(a,b,c)(h) body = %q, a(b(c(h))) body = %q, want equal", got, want)
	}
}

func TestChainClonesInput(t *testing.T) {
	ss := []func(http.Handler) http.Handler{wrapHeader("X-Wrapped", "1")}
	c := Chain(ss...)
	// Mutating the caller's slice after Chain must not change the composition.
	ss[0] = wrapHeader("X-Wrapped", "2")
	rec := serve(t, c(markHandler("h")), http.MethodGet, "/")
	if got := rec.Header().Get("X-Wrapped"); got != "1" {
		t.Errorf("composition changed after source slice mutation: got %q, want %q", got, "1")
	}
}
