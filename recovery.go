// Copyright (c) 2026 The Sim Authors
// Use of this source code is governed by a MIT license
// that can be found in the LICENSE file.

package sim

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"runtime/debug"
	"syscall"
)

// Recovery wraps an http.Handler to recover from panics, logging them
// with a stack trace and writing error responses via PanicHandler.
type Recovery struct {
	// PanicHandler is called after a panic is recovered to handle it,
	// typically by writing the HTTP response. For most panics, Recovery
	// already logs the stack trace; PanicHandler only needs to take care of
	// the response (and optional side‑effects such as error reporting).
	// Connection‑related panics are handled internally and never invoke this handler.
	// If the response was already committed prior to the panic, net/http
	// ignores further WriteHeader calls and appends further writes to the body.
	// Implementations should be aware of this behavior.
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
// invoking h and logs them via slog.
//
// If a panic is recovered, the error response is written by the handler
// set in [Recovery.PanicHandler]. Panics whose value is or wraps
// [http.ErrAbortHandler] are re-panicked so net/http can abort the
// connection silently, and panics caused by a dead connection (such as
// a reset or a broken pipe) are logged as warnings without writing a
// response.
func (rc *Recovery) Handler(h http.Handler) http.Handler {
	panicHandler := defaultPanicHandler
	if rc.PanicHandler != nil {
		panicHandler = rc.PanicHandler
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		//nolint:contextcheck // ctx must be read from r at panic time
		// defer args are evaluated eagerly and would lose downstream context values
		defer func() {
			value := recover()
			if value == nil {
				return
			}
			err, ok := value.(error)
			if ok && errors.Is(err, http.ErrAbortHandler) {
				// Let net/http abort it silently — no stack trace. Re-panic the
				// sentinel itself so net/http's identity check still matches even
				// when the recovered value is wrapped (e.g. by singleflight).
				// See: https://github.com/golang/go/issues/56228
				// See: https://github.com/golang/go/issues/62510
				panic(http.ErrAbortHandler)
			}
			if ok && (errors.Is(err, syscall.ECONNRESET) ||
				errors.Is(err, syscall.EPIPE) ||
				errors.Is(err, net.ErrClosed) ||
				errors.Is(err, syscall.ECONNABORTED)) {
				slog.WarnContext(r.Context(), "[Recovery] client disconnected",
					slog.Any("err", err),
					slog.GroupAttrs("request",
						slog.String("pattern", r.Pattern),
						slog.String("method", r.Method),
						slog.String("path", r.URL.Path)),
				)
				return
			}
			panicError := &PanicError{Value: value, Stack: debug.Stack()}
			slog.ErrorContext(r.Context(), "[Recovery] panic recovered",
				slog.Any("err", panicError),
				slog.GroupAttrs("request",
					slog.String("pattern", r.Pattern),
					slog.String("method", r.Method),
					slog.String("path", r.URL.Path),
					slog.String("dump", redactedRequestDump(r))),
			)
			panicHandler(w, r, panicError)
		}()
		h.ServeHTTP(w, r)
	})
}

// redactedRequestDump renders r for logging, masking credential headers
// (Authorization, Proxy-Authorization). It operates on a clone so the
// live request's headers are left untouched.
func redactedRequestDump(r *http.Request) string {
	r2 := r.Clone(r.Context())
	for _, h := range []string{"Authorization", "Proxy-Authorization"} {
		if r2.Header.Get(h) != "" {
			r2.Header.Set(h, "*")
		}
	}
	dump, err := httputil.DumpRequest(r2, false)
	if err != nil {
		return fmt.Sprintf("<dump request failed: %v>", err)
	}
	return string(dump)
}

// PanicError carries the value and stack trace of a recovered panic.
type PanicError struct {
	// Value is the value passed to panic. It may not be an error.
	Value any
	// Stack is the goroutine stack trace captured at the recovery point.
	Stack []byte
}

// Error implements the error interface.
func (p *PanicError) Error() string {
	return fmt.Sprintf("panic caught: %v", p.Value)
}

// Unwrap returns the panic value if it is an error, enabling
// errors.Is, errors.As, and errors.AsType to match against it.
func (p *PanicError) Unwrap() error {
	if err, ok := p.Value.(error); ok {
		return err
	}
	return nil
}

// LogValue implements the slog.LogValuer interface.
func (p *PanicError) LogValue() slog.Value {
	return slog.GroupValue(
		slog.Any("panic", p.Value),
		slog.String("stack", string(p.Stack)),
	)
}
