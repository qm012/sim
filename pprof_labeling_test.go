// Copyright (c) 2026 The Sim Authors
// Use of this source code is governed by a MIT license
// that can be found in the LICENSE file.

package sim_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"runtime/pprof"
	"testing"

	"github.com/qm012/sim"
)

// serveLabeled serves req through pl.Handler and returns the pprof
// labels visible to the wrapped handler's context.
func serveLabeled(t *testing.T, pl *sim.PprofLabeling, req *http.Request) map[string]string {
	t.Helper()
	got := map[string]string{}
	h := pl.Handler(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		pprof.ForLabels(r.Context(), func(key, value string) bool {
			got[key] = value
			return true
		})
	}))
	h.ServeHTTP(httptest.NewRecorder(), req)
	return got
}

func TestPprofLabeling(t *testing.T) {
	type ctxKey struct{}
	tests := []struct {
		name string
		pl   sim.PprofLabeling
		req  func() *http.Request
		want map[string]string
	}{
		{
			name: "default labels the matched pattern",
			req: func() *http.Request {
				req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/users/42", nil)
				req.Pattern = "GET /users/{id}"
				return req
			},
			want: map[string]string{"pattern": "GET /users/{id}"},
		},
		{
			name: "custom labels replace the default",
			pl: sim.PprofLabeling{Labels: func(*http.Request) []string {
				return []string{"method", http.MethodGet, "tenant", "acme"}
			}},
			req: func() *http.Request {
				req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
				req.Pattern = "GET /"
				return req
			},
			want: map[string]string{"method": http.MethodGet, "tenant": "acme"},
		},
		{
			name: "labels read from the request context",
			pl: sim.PprofLabeling{Labels: func(r *http.Request) []string {
				id, _ := r.Context().Value(ctxKey{}).(string)
				return []string{"trace_id", id}
			}},
			req: func() *http.Request {
				ctx := context.WithValue(t.Context(), ctxKey{}, "t-1")
				return httptest.NewRequestWithContext(ctx, http.MethodGet, "/", nil)
			},
			want: map[string]string{"trace_id": "t-1"},
		},
		{
			name: "custom labels returning none drop the default",
			pl:   sim.PprofLabeling{Labels: func(*http.Request) []string { return nil }},
			req: func() *http.Request {
				req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
				req.Pattern = "GET /"
				return req
			},
			want: map[string]string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := serveLabeled(t, &tt.pl, tt.req()); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("labels = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPprofLabelingCapturesFieldsAtCallTime(t *testing.T) {
	pl := &sim.PprofLabeling{Labels: func(*http.Request) []string {
		return []string{"trace_id", "captured"}
	}}
	handler := pl.Handler(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got := map[string]string{}
		pprof.ForLabels(r.Context(), func(key, value string) bool {
			got[key] = value
			return true
		})
		if got["trace_id"] != "captured" {
			t.Errorf("trace_id = %q, want %q (captured at call time)", got["trace_id"], "captured")
		}
	}))

	// Mutate after Handler; the returned handler keeps the captured values.
	pl.Labels = func(*http.Request) []string {
		return []string{"trace_id", "mutated"}
	}
	handler.ServeHTTP(httptest.NewRecorder(),
		httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))
}

func TestPprofLabelingPropagatesPanic(t *testing.T) {
	handler := (&sim.PprofLabeling{}).Handler(http.HandlerFunc(
		func(http.ResponseWriter, *http.Request) { panic("boom") }))

	defer func() {
		if recover() == nil {
			t.Error("panic did not propagate")
		}
	}()
	handler.ServeHTTP(httptest.NewRecorder(),
		httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))
}

func ExamplePprofLabeling() {
	// Nil Labels defaults to a single "pattern" label; assign a Labels
	// function to add per-request pairs such as a trace ID, and
	// register the wrapper with Use, before Recovery.
	pl := &sim.PprofLabeling{Labels: func(r *http.Request) []string {
		return []string{"pattern", r.Pattern, "trace_id", "acme"}
	}}
	handler := pl.Handler(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		pprof.ForLabels(r.Context(), func(key, value string) bool {
			fmt.Println(key, value)
			return true
		})
	}))

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
	req.Pattern = "GET /"
	handler.ServeHTTP(httptest.NewRecorder(), req)
	// Output:
	// pattern GET /
	// trace_id acme
}
