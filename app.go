// Copyright (c) 2026 qm012<1007661792@qq.com>. All rights reserved.
// Use of this source code is governed by a MIT license
// that can be found in the LICENSE file.

package sim

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"
)

// App is an HTTP router built on top of [http.ServeMux], extending
// it with method-based routing helpers such as [App.Get], [App.Post]
// and [App.Any]. Requests are matched against registered patterns
// using the same syntax and precedence rules as [http.ServeMux].
// App implements [http.Handler]; create one with [NewApp].
type App struct {
	// mux routes requests to registered handlers. Pattern matching
	// and precedence follow [http.ServeMux].
	mux *http.ServeMux

	// ss holds the wrappers applied to every registered handler.
	// They wrap in order: ss[0] is outermost, receives the request
	// first, and its response is what the caller ultimately sees —
	// the same composition as [Chain].
	ss []func(http.Handler) http.Handler
}

// NewApp returns a new [App] value.
func NewApp() *App {
	return &App{mux: http.NewServeMux()}
}

var (
	_ Router       = (*App)(nil)
	_ http.Handler = (*App)(nil)
)

// Use registers the given wrappers and applies them to every handler
// registered after this call. Wrappers run in registration order:
// the first is outermost and receives the request first, the same
// composition as [Chain].
func (a *App) Use(ss ...func(http.Handler) http.Handler) {
	a.ss = append(a.ss, ss...)
}

// Handle registers the handler for the given pattern, with the same
// behavior as [http.ServeMux.Handle] and [http.Handle].
func (a *App) Handle(pattern string, handler http.Handler) {
	a.handle(pattern, handler)
}

// HandleFunc registers the handler function for the given pattern,
// with the same behavior as [http.ServeMux.HandleFunc] and [http.HandleFunc].
func (a *App) HandleFunc(pattern string, handlerFunc http.HandlerFunc) {
	a.handle(pattern, handlerFunc)
}

// Any registers handlerFunc for the given path, matching all HTTP methods.
func (a *App) Any(path string, handlerFunc http.HandlerFunc) {
	for _, method := range allMethods {
		a.handle(method+" "+path, handlerFunc)
	}
}

// Get registers handlerFunc for GET requests to the given path.
func (a *App) Get(path string, handlerFunc http.HandlerFunc) {
	a.handle(http.MethodGet+" "+path, handlerFunc)
}

// Post registers handlerFunc for POST requests to the given path.
func (a *App) Post(path string, handlerFunc http.HandlerFunc) {
	a.handle(http.MethodPost+" "+path, handlerFunc)
}

// Delete registers handlerFunc for DELETE requests to the given path.
func (a *App) Delete(path string, handlerFunc http.HandlerFunc) {
	a.handle(http.MethodDelete+" "+path, handlerFunc)
}

// Patch registers handlerFunc for PATCH requests to the given path.
func (a *App) Patch(path string, handlerFunc http.HandlerFunc) {
	a.handle(http.MethodPatch+" "+path, handlerFunc)
}

// Put registers handlerFunc for PUT requests to the given path.
func (a *App) Put(path string, handlerFunc http.HandlerFunc) {
	a.handle(http.MethodPut+" "+path, handlerFunc)
}

// Options registers handlerFunc for OPTIONS requests to the given path.
func (a *App) Options(path string, handlerFunc http.HandlerFunc) {
	a.handle(http.MethodOptions+" "+path, handlerFunc)
}

// Head registers handlerFunc for HEAD requests to the given path.
func (a *App) Head(path string, handlerFunc http.HandlerFunc) {
	a.handle(http.MethodHead+" "+path, handlerFunc)
}

// Connect registers handlerFunc for CONNECT requests to the given path.
func (a *App) Connect(path string, handlerFunc http.HandlerFunc) {
	a.handle(http.MethodConnect+" "+path, handlerFunc)
}

// Trace registers handlerFunc for TRACE requests to the given path.
func (a *App) Trace(path string, handlerFunc http.HandlerFunc) {
	a.handle(http.MethodTrace+" "+path, handlerFunc)
}

// Handler returns the handler and the matching pattern for the given request.
func (a *App) Handler(r *http.Request) (http.Handler, string) {
	return a.mux.Handler(r)
}

// handle registers h on the mux for the given pattern.
// The pattern may include a method prefix, e.g. "GET /path" (see
// [http.ServeMux]); without one, h is registered for all methods.
func (a *App) handle(pattern string, h http.Handler) {
	a.mux.Handle(pattern, Chain(a.ss...)(h))
	slog.Debug("route registered", "pattern", pattern)
}

// ServeHTTP implements the http.Handler interface.
func (a *App) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.mux.ServeHTTP(w, r)
}

// shutdownTimeout bounds how long a graceful shutdown may wait for
// in-flight requests to complete.
const shutdownTimeout = 10 * time.Second

// Run listens on the given TCP address and serves HTTP requests until ctx
// is canceled or the server fails. If ctx is canceled, Run shuts the
// server down gracefully and returns nil.
func (a *App) Run(ctx context.Context, addr string) error {
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", addr, err)
	}
	slog.DebugContext(ctx, "listening and serving HTTP", "addr", ln.Addr().String())
	server := &http.Server{ // #nosec G112
		Handler: a,
	}
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Serve(ln)
	}()

	select {
	case err = <-errCh:
		return fmt.Errorf("http serve: %w", err)
	case <-ctx.Done():
	}

	slog.InfoContext(context.WithoutCancel(ctx), "shutting down HTTP server", "addr", ln.Addr().String())
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	//nolint:contextcheck // shutdown must use a fresh context; the incoming ctx is already canceled
	if err = server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}
	slog.Info("HTTP server shut down gracefully")
	return nil
}
