// Copyright (c) 2026 The Sim Authors
// Use of this source code is governed by a MIT license
// that can be found in the LICENSE file.

package sim_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/qm012/sim"
)

// captureLogs runs fn with the default slog logger redirected to a
// Debug-level JSON handler and returns the last record it emits.
func captureLogs(t *testing.T, fn func()) map[string]any {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	fn()

	var rec map[string]any
	dec := json.NewDecoder(&buf)
	for dec.More() {
		rec = nil // Decode into a non-nil map merges keys; reset per record.
		if err := dec.Decode(&rec); err != nil {
			t.Fatalf("log record is not valid JSON: %v\n%s", err, buf.String())
		}
	}
	if rec == nil {
		t.Fatalf("no log records captured")
	}
	return rec
}

// serveLogged serves a GET target through rl.Handler(h) and returns the
// captured log record. pattern is faked on the request, as ServeMux would.
func serveLogged(t *testing.T, rl *sim.RequestLogger, h http.Handler, target, pattern string) map[string]any {
	t.Helper()
	return captureLogs(t, func() {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, target, nil)
		req.Pattern = pattern
		rl.Handler(h).ServeHTTP(httptest.NewRecorder(), req)
	})
}

func requestGroup(t *testing.T, rec map[string]any) map[string]any {
	t.Helper()
	g, ok := rec["request"].(map[string]any)
	if !ok {
		t.Fatalf("log record has no request group: %v", rec)
	}
	return g
}

//nolint:funlen // table-driven assertions over captured log records
func TestLoggingRecord(t *testing.T) {
	const wantMsg = "served http request"
	tests := []struct {
		name       string
		target     string
		pattern    string
		handler    http.HandlerFunc
		wantStatus int
		wantLevel  string
	}{
		{
			name:    "status and message",
			target:  "/a",
			pattern: "GET /a",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte("hi"))
			},
			wantStatus: http.StatusCreated,
			wantLevel:  "DEBUG",
		},
		{
			name:       "no response written defaults to 200",
			target:     "/b",
			handler:    func(_ http.ResponseWriter, _ *http.Request) {},
			wantStatus: http.StatusOK,
			wantLevel:  "DEBUG",
		},
		{
			name:   "client error warns",
			target: "/c",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
			wantStatus: http.StatusNotFound,
			wantLevel:  "WARN",
		},
		{
			name:   "server error escalates",
			target: "/d",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
			wantStatus: http.StatusInternalServerError,
			wantLevel:  "ERROR",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := serveLogged(t, sim.NewRequestLogger(), tt.handler, tt.target, tt.pattern)
			if got := rec["msg"]; got != wantMsg {
				t.Errorf("msg = %v, want %q", got, wantMsg)
			}
			if got := rec["level"]; got != tt.wantLevel {
				t.Errorf("level = %v, want %q", got, tt.wantLevel)
			}
			if got := rec["status_code"]; got != float64(tt.wantStatus) {
				t.Errorf("status_code = %v, want %d", got, tt.wantStatus)
			}
			if _, ok := rec["cost_duration"].(float64); !ok {
				t.Errorf("cost_duration missing: %v", rec)
			}
			g := requestGroup(t, rec)
			if got := g["method"]; got != http.MethodGet {
				t.Errorf("request.method = %v, want %q", got, http.MethodGet)
			}
			if got := g["uri"]; got != tt.target {
				t.Errorf("request.uri = %v, want %q", got, tt.target)
			}
			if got := g["pattern"]; got != tt.pattern {
				t.Errorf("request.pattern = %v, want %q", got, tt.pattern)
			}
		})
	}
}

func TestLoggingBytesWritten(t *testing.T) {
	const body = "hello"
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	})
	t.Run("opt-in records bytes", func(t *testing.T) {
		rl := sim.NewRequestLogger()
		rl.LogBytesWritten = true
		rec := serveLogged(t, rl, handler, "/", "")
		if got := rec["bytes_written"]; got != float64(len(body)) {
			t.Errorf("bytes_written = %v, want %d", got, len(body))
		}
	})
	t.Run("off by default", func(t *testing.T) {
		rec := serveLogged(t, sim.NewRequestLogger(), handler, "/", "")
		if _, ok := rec["bytes_written"]; ok {
			t.Errorf("bytes_written present by default: %v", rec)
		}
	})
}

func TestLoggingExtraAttrs(t *testing.T) {
	rl := sim.NewRequestLogger()
	rl.ExtraAttrs = func(_ *http.Request) []slog.Attr {
		return []slog.Attr{slog.String("user_id", "u1")}
	}
	rec := serveLogged(t, rl, http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}), "/", "")
	if got := rec["user_id"]; got != "u1" {
		t.Errorf("user_id = %v, want %q", got, "u1")
	}
}

func TestLoggingPreservesFlusher(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		f, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "not a flusher", http.StatusInternalServerError)
			return
		}
		f.Flush()
		_, _ = w.Write([]byte("ok"))
	})
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	captured := captureLogs(t, func() {
		sim.NewRequestLogger().Handler(handler).ServeHTTP(rec, req)
	})
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body = %q", rec.Code, rec.Body.String())
	}
	if !rec.Flushed {
		t.Error("response was not flushed")
	}
	if got := captured["status_code"]; got != float64(http.StatusOK) {
		t.Errorf("status_code = %v, want %d", got, http.StatusOK)
	}
}

func TestLoggingHideQueryString(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	t.Run("default records full uri", func(t *testing.T) {
		rec := serveLogged(t, sim.NewRequestLogger(), handler, "/search?q=go", "")
		if got := requestGroup(t, rec)["uri"]; got != "/search?q=go" {
			t.Errorf("uri = %v, want %q", got, "/search?q=go")
		}
	})
	t.Run("hidden omits query", func(t *testing.T) {
		rl := sim.NewRequestLogger()
		rl.HideQueryString = true
		rec := serveLogged(t, rl, handler, "/search?q=go&token=abc", "")
		if got := requestGroup(t, rec)["uri"]; got != "/search" {
			t.Errorf("uri = %v, want %q", got, "/search")
		}
	})
}
