// Copyright (c) 2026 The Sim Authors
// Use of this source code is governed by a MIT license
// that can be found in the LICENSE file.

package sim

import (
	"context"
	"net/http"
	"runtime/pprof"
)

// PprofLabeling unconditionally labels every request it serves via
// [runtime/pprof.Do]. The labels ride along with the request's
// goroutine, so an active CPU profile and goroutine tracebacks since
// Go 1.27 carry them (GODEBUG=tracebacklabels=0 disables the
// latter), and panics recovered by [Recovery] are attributed to
// their route.
//
// Register it after any wrapper whose values the Labels function reads
// from the request context, such as a trace ID wrapper, since the
// context is only populated by wrappers registered earlier in the chain.
// Prefer registering it before [Recovery] so samples taken while
// recovering from panics carry the labels too; [Default] does not
// register PprofLabeling, so compose the chain with [App.Use] as shown
// on [Default].
type PprofLabeling struct {
	// Labels returns the key/value pairs applied to each request.
	// Nil applies a single "pattern" label holding the matched pattern.
	// Keep the cardinality bounded: a unique value per request, such as
	// a trace ID, grows the profile linearly with the request count
	// during the collection window.
	Labels func(*http.Request) []string
}

// Handler wraps h and serves each request inside [runtime/pprof.Do].
// It captures the current field values at call time;
// later changes do not affect the returned handler.
func (p *PprofLabeling) Handler(h http.Handler) http.Handler {
	labels := p.Labels
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		kv := []string{"pattern", r.Pattern}
		if labels != nil {
			kv = labels(r)
		}
		pprof.Do(r.Context(), pprof.Labels(kv...), func(ctx context.Context) {
			h.ServeHTTP(w, r.WithContext(ctx))
		})
	})
}
