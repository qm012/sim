// Copyright (c) 2026 The Sim Authors
// Use of this source code is governed by a MIT license
// that can be found in the LICENSE file.

package sim

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"runtime/debug"
	"syscall"
)

// Recovery wraps an http.Handler to recover from panics, logging them
// with a stack trace and writing the error response via PanicHandler.
type Recovery struct {
	// PanicHandler is called after a panic is recovered to handle it,
	// typically by writing the HTTP response. The panic has already been
	// logged by the framework; PanicHandler only needs to take care of
	// the response (and optional side effects like error reporting).
	// Connection errors are handled internally and never reach it.
	// If nil, defaultPanicHandler is used.
	PanicHandler func(http.ResponseWriter, *http.Request, *PanicError)
}

// NewRecovery returns a new Recovery.
func NewRecovery() *Recovery {
	return &Recovery{}
}

func defaultPanicHandler(w http.ResponseWriter, _ *http.Request, _ *PanicError) {
	http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
}

// Handler returns a handler that recovers from panics raised while
// invoking the handler h and logs them via slog.
//
// If a panic is recovered, the error response is written by the
// handler set in [Recovery.PanicHandler]. Panics with [http.ErrAbortHandler]
// are re-panicked so net/http can abort the connection silently, and
// panics caused by a dead connection (EPIPE, ECONNRESET) are logged
// as warnings without writing a response.
func (rc *Recovery) Handler(h http.Handler) http.Handler {
	panicHandler := defaultPanicHandler
	if rc.PanicHandler != nil {
		panicHandler = rc.PanicHandler
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			rec := recover()
			if rec == nil {
				return
			}
			if rec == http.ErrAbortHandler {
				// Let net/http abort it silently — no stack trace.
				// See: https://github.com/golang/go/issues/56228
				panic(rec)
			}
			if err, ok := rec.(error); ok && (errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.EPIPE)) {
				slog.WarnContext(r.Context(), "[Recovery] client disconnected", "err", err,
					slog.GroupAttrs("request",
						slog.String("pattern", r.Pattern),
						slog.String("method", r.Method),
						slog.String("path", r.URL.Path)),
				)
				return
			}
			panicError := &PanicError{Panic: rec, Stack: debug.Stack()}
			slog.ErrorContext(r.Context(), "[Recovery] panic recovered",
				"err", panicError,
				slog.GroupAttrs("request",
					slog.String("pattern", r.Pattern),
					slog.String("method", r.Method),
					slog.String("path", r.URL.Path),
					slog.String("dump", secureRequestDump(r))),
			)
			panicHandler(w, r, panicError)
		}()
		h.ServeHTTP(w, r)
	})
}

// secureRequestDump renders r for logging, masking credential headers
// (Authorization, Proxy-Authorization). It operates on a clone so the
// live request's headers are left untouched.
func secureRequestDump(r *http.Request) string {
	req := r.Clone(r.Context())
	for _, h := range []string{"Authorization", "Proxy-Authorization"} {
		if req.Header.Get(h) != "" {
			req.Header.Set(h, "*")
		}
	}
	dump, err := httputil.DumpRequest(req, false)
	if err != nil {
		return fmt.Sprintf("<dump request failed: %v>", err)
	}
	return string(dump)
}

// PanicError carries the value and stack trace of a recovered panic.
type PanicError struct {
	// Panic is the value passed to panic. It may not be an error.
	Panic any
	// Stack is the goroutine stack trace captured at the recovery point.
	Stack []byte
}

func (p *PanicError) Error() string {
	return fmt.Sprintf("panic caught: %v", p.Panic)
}

// LogValue implements the slog.LogValuer interface.
func (p *PanicError) LogValue() slog.Value {
	return slog.GroupValue(
		slog.Any("panic", p.Panic),
		slog.String("stack", string(p.Stack)),
	)
}
