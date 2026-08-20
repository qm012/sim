// Copyright (c) 2026 The Sim Authors
// Use of this source code is governed by a MIT license
// that can be found in the LICENSE file.

package sim

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"syscall"
	"testing"
)

func panicHandler(value any) http.Handler {
	return http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic(value)
	})
}

// serveRecovery serves h through rc.Handler and recovers any propagated panic.
func serveRecovery(t *testing.T, rc *Recovery, h http.Handler) (*httptest.ResponseRecorder, any) {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	var recovered any
	func() {
		defer func() { recovered = recover() }()
		rc.Handler(h).ServeHTTP(rec, req)
	}()
	return rec, recovered
}

func TestRecoveryPassesThroughNormalRequests(t *testing.T) {
	rc := NewRecovery()
	rc.HandlePanic = func(http.ResponseWriter, *http.Request, *PanicError) {
		t.Error("HandlePanic called for a request without a panic")
	}
	rec, recovered := serveRecovery(t, rc, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	if recovered != nil {
		t.Fatalf("panic propagated: %#v", recovered)
	}
	if rec.Code != http.StatusOK || rec.Body.String() != "ok" {
		t.Errorf("code/body = %d %q, want 200 %q", rec.Code, rec.Body.String(), "ok")
	}
}

func TestRecoveryDefaultHandlePanic(t *testing.T) {
	rec, _ := serveRecovery(t, NewRecovery(), panicHandler("boom"))
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("code = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	if want := http.StatusText(http.StatusInternalServerError) + "\n"; rec.Body.String() != want {
		t.Errorf("body = %q, want %q", rec.Body.String(), want)
	}
}

func TestRecoveryCustomHandlePanic(t *testing.T) {
	var (
		gotPE  *PanicError
		gotReq *http.Request
	)
	rc := NewRecovery()
	rc.HandlePanic = func(w http.ResponseWriter, r *http.Request, pe *PanicError) {
		gotPE = pe
		gotReq = r
		w.WriteHeader(http.StatusTeapot)
	}
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/panic", nil)
	rec := httptest.NewRecorder()
	rc.Handler(panicHandler("boom")).ServeHTTP(rec, req)

	if gotPE == nil {
		t.Fatal("HandlePanic not called")
	}
	if gotReq != req {
		t.Error("HandlePanic received a different request")
	}
	if !strings.Contains(string(gotPE.Stack), "recovery_test.go") {
		t.Errorf("Stack does not contain the panic site:\n%s", gotPE.Stack)
	}
	if got, want := gotPE.Error(), "panic caught: boom"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
	if rec.Code != http.StatusTeapot {
		t.Errorf("code = %d, want %d", rec.Code, http.StatusTeapot)
	}
}

func TestRecoveryReadsRequestAtPanicTime(t *testing.T) {
	type ctxKey struct{}
	var gotReq *http.Request
	rc := NewRecovery()
	rc.HandlePanic = func(_ http.ResponseWriter, r *http.Request, _ *PanicError) {
		gotReq = r
	}
	h := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		*r = *r.WithContext(context.WithValue(r.Context(), ctxKey{}, "downstream"))
		panic("boom")
	})
	serveRecovery(t, rc, h)
	if gotReq == nil {
		t.Fatal("HandlePanic not called")
	}
	if got := gotReq.Context().Value(ctxKey{}); got != "downstream" {
		t.Errorf("context value at panic time = %v, want %q", got, "downstream")
	}
}

type panicAsError struct{ msg string }

func (e *panicAsError) Error() string { return e.msg }

func TestRecoveryPanicValues(t *testing.T) {
	typed := &panicAsError{msg: "as-boom"}
	tests := []struct {
		name   string
		value  any
		unwrap error // expected Unwrap() result; nil when the value is not an error
		as     bool  // errors.AsType to *panicAsError must succeed
	}{
		{"string", "boom", nil, false},
		{"error", io.ErrUnexpectedEOF, io.ErrUnexpectedEOF, false},
		{"wrapped error", fmt.Errorf("wrapped: %w", io.ErrUnexpectedEOF), io.ErrUnexpectedEOF, false},
		{"typed error", typed, typed, true},
		{"struct", struct{ X int }{X: 1}, nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotPE *PanicError
			rc := NewRecovery()
			rc.HandlePanic = func(_ http.ResponseWriter, _ *http.Request, pe *PanicError) {
				gotPE = pe
			}
			serveRecovery(t, rc, panicHandler(tt.value))

			if gotPE == nil {
				t.Fatal("HandlePanic not called")
			}
			if gotPE.Value != tt.value {
				t.Errorf("Value = %v (%T), want %v (%T)", gotPE.Value, gotPE.Value, tt.value, tt.value)
			}
			got := gotPE.Unwrap()
			if tt.unwrap == nil {
				if got != nil {
					t.Errorf("Unwrap() = %v, want nil", got)
				}
			} else {
				if !errors.Is(got, tt.unwrap) {
					t.Errorf("errors.Is(Unwrap(), %v) = false, want true", tt.unwrap)
				}
				if !errors.Is(gotPE, tt.unwrap) {
					t.Errorf("errors.Is(pe, %v) = false, want true", tt.unwrap)
				}
			}
			if tt.as {
				if target, ok := errors.AsType[*panicAsError](gotPE); !ok || !errors.Is(target, typed) {
					t.Errorf("errors.AsType[*panicAsError](gotPE) = %v, want %v", target, typed)
				}
			}
		})
	}
}

func TestRecoverySpecialPanics(t *testing.T) {
	tests := []struct {
		name      string
		value     any
		recovered any // nil = swallowed without a response; else re-panicked as-is
	}{
		{"bare ErrAbortHandler", http.ErrAbortHandler, http.ErrAbortHandler},
		{"wrapped ErrAbortHandler", fmt.Errorf("abort: %w", http.ErrAbortHandler), http.ErrAbortHandler},
		{"EPIPE", syscall.EPIPE, nil},
		{"ECONNRESET", syscall.ECONNRESET, nil},
		{"net.ErrClosed", net.ErrClosed, nil},
		{"wrapped net.ErrClosed", fmt.Errorf("wrapped: %w", net.ErrClosed), nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rc := NewRecovery()
			rc.HandlePanic = func(http.ResponseWriter, *http.Request, *PanicError) {
				t.Error("HandlePanic must not be called")
			}
			rec, recovered := serveRecovery(t, rc, panicHandler(tt.value))
			if tt.recovered == nil {
				if recovered != nil {
					t.Errorf("panic propagated: %#v", recovered)
				}
			} else if recovered != tt.recovered {
				t.Errorf("recovered value = %#v, want the %v sentinel", recovered, tt.recovered)
			}
			if rec.Code != http.StatusOK {
				t.Errorf("code = %d, want default %d (no response written)", rec.Code, http.StatusOK)
			}
			if rec.Body.Len() != 0 {
				t.Errorf("body = %q, want empty", rec.Body.String())
			}
		})
	}
}

func TestPanicErrorLogValue(t *testing.T) {
	stack := []byte("goroutine 1 [running]:\nrecovery_test.go:42\n")
	p := &PanicError{Value: "boom", Stack: stack}
	attrs := p.LogValue().Group()
	got := make(map[string]slog.Value, len(attrs))
	for _, a := range attrs {
		got[a.Key] = a.Value
	}
	if v := got["panic"]; v.String() != "boom" {
		t.Errorf("panic attr = %v, want %q", v, "boom")
	}
	if v := got["stack"]; v.String() != string(stack) {
		t.Errorf("stack attr = %v, want %q", v, stack)
	}
}

func TestRedactedRequestDump(t *testing.T) {
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/secret?token=abc", nil)
	req.Header.Set("Authorization", "Bearer hunter2")
	req.Header.Set("Proxy-Authorization", "Basic dXNlcjpwYXNz")
	req.Header.Set("Cookie", "session=topsecret; theme=dark")
	req.Header.Set("X-Custom", "visible")

	dump := redactedRequestDump(req)

	for _, secret := range []string{"hunter2", "dXNlcjpwYXNz", "topsecret"} {
		if strings.Contains(dump, secret) {
			t.Errorf("dump leaks %q: %q", secret, dump)
		}
	}
	for _, masked := range []string{"Authorization: *", "Proxy-Authorization: *", "Cookie: *"} {
		if !strings.Contains(dump, masked) {
			t.Errorf("dump does not mask %q: %q", masked, dump)
		}
	}
	if !strings.Contains(dump, "X-Custom: visible") {
		t.Errorf("dump missing non-credential header: %q", dump)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer hunter2" {
		t.Errorf("original Authorization = %q, want %q", got, "Bearer hunter2")
	}
	if got := req.Header.Get("Proxy-Authorization"); got != "Basic dXNlcjpwYXNz" {
		t.Errorf("original Proxy-Authorization = %q, want unchanged", got)
	}
	if got := req.Header.Get("Cookie"); got != "session=topsecret; theme=dark" {
		t.Errorf("original Cookie = %q, want unchanged", got)
	}
}

func TestRecoveryNilURLRequest(t *testing.T) {
	tests := []struct {
		name  string
		value any
		code  int
	}{
		{"panic", "boom", http.StatusInternalServerError},
		{"client disconnect", net.ErrClosed, http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			r := &http.Request{Method: http.MethodGet} // URL is nil
			defer func() {
				if v := recover(); v != nil {
					t.Fatalf("secondary panic escaped Recovery: %v", v)
				}
			}()
			NewRecovery().Handler(panicHandler(tt.value)).ServeHTTP(rec, r)
			if rec.Code != tt.code {
				t.Errorf("code = %d, want %d", rec.Code, tt.code)
			}
		})
	}
}

func TestRecoveryLogging(t *testing.T) {
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })

	t.Run("panic", func(t *testing.T) {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/secret", nil)
		req.Header.Set("Authorization", "Bearer hunter2")
		rec := httptest.NewRecorder()
		NewRecovery().Handler(panicHandler("boom")).ServeHTTP(rec, req)

		if rec.Code != http.StatusInternalServerError {
			t.Errorf("code = %d, want %d", rec.Code, http.StatusInternalServerError)
		}
		out := buf.String()
		for _, want := range []string{
			"[Recovery] panic recovered", "boom", "method=POST", "path=/secret", "Authorization: *",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("log missing %q:\n%s", want, out)
			}
		}
		if strings.Contains(out, "hunter2") {
			t.Errorf("log leaks Authorization secret:\n%s", out)
		}
	})

	t.Run("client disconnect", func(t *testing.T) {
		buf.Reset()
		serveRecovery(t, NewRecovery(), panicHandler(net.ErrClosed))
		if out := buf.String(); !strings.Contains(out, "[Recovery] client disconnected") {
			t.Errorf("log missing %q:\n%s", "[Recovery] client disconnected", out)
		}
	})
}
