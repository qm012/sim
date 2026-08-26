// Copyright (c) 2026 The Sim Authors
// Use of this source code is governed by a MIT license
// that can be found in the LICENSE file.

package sim

import (
	"net/http"
)

// Selector returns a wrapper that conditionally applies the wrapper s
// to each request:
//
//   - match(r) == true: the request runs through s;
//   - match(r) == false: s is skipped entirely.
//
// s is applied once, when the returned wrapper is built, not on
// every request.
//
// The returned wrapper composes with [Chain] and [App.Use]. For example,
// to log only requests under /api:
//
//	app.Use(sim.Selector(logging.Handler, func(r *http.Request) bool {
//		return strings.HasPrefix(r.URL.Path, "/api")
//	}))
//
// Or to skip logging for health checks:
//
//	app.Use(sim.Selector(logging.Handler, func(r *http.Request) bool {
//		return r.URL.Path != "/healthz"
//	}))
//
// As an element of [Chain], it conditions one wrapper without affecting
// the others:
//
//	sim.Chain(
//		sim.Selector(logging.Handler, func(r *http.Request) bool {
//			return r.URL.Path != "/healthz"
//		}),
//		new(Recovery).Handler,
//	)
func Selector(s func(http.Handler) http.Handler, match func(*http.Request) bool) func(http.Handler) http.Handler {
	return func(h http.Handler) http.Handler {
		wrapped := s(h)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if match(r) {
				wrapped.ServeHTTP(w, r)
				return
			}
			h.ServeHTTP(w, r)
		})
	}
}
