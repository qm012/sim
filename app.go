// Copyright (c) 2026 The Sim Authors
// Use of this source code is governed by a MIT license
// that can be found in the LICENSE file.

package sim

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"path"
	"slices"
	"strings"
	"time"
)

// App is an HTTP router built on top of [http.ServeMux], extending
// it with method-based routing helpers such as [App.Get], [App.Post]
// and [App.Any]. Requests are matched against registered patterns
// using the same syntax and precedence rules as [http.ServeMux].
// App implements [http.Handler]; create one with [NewApp].
type App struct {
	// basePath is the path prefix prepended to every pattern
	// registered on this router.
	basePath string

	// mux routes requests; pattern matching and precedence follow
	// [http.ServeMux].
	mux *http.ServeMux

	// ss holds the wrappers applied to every registered handler.
	// They wrap in order: ss[0] is outermost, receives the request
	// first, and its response is what the caller ultimately sees —
	// the same composition as [Chain].
	ss []func(http.Handler) http.Handler

	// Handlers served when a request matches no registered pattern
	// (404) or matches a pattern but not its method (405); nil keeps
	// the mux defaults. See: https://github.com/golang/go/issues/65648
	notFoundHandler         http.Handler
	methodNotAllowedHandler http.Handler
}

var (
	_ Router       = (*App)(nil)
	_ http.Handler = (*App)(nil)
)

// NewApp returns a new [App] value.
func NewApp() *App {
	return &App{mux: http.NewServeMux()}
}

// Default returns a new [App] with the standard wrappers already
// registered by [App.Use], outermost first:
//
//   - [ClientIPResolution] resolves the client IP into the request context.
//   - [RequestLogging] logs each request, including the resolved client_ip.
//   - [Recovery] recovers panics raised by the handler.
//
// The order is fixed by the wrappers themselves: ClientIPResolution must
// run before RequestLogging reads the client IP, and RequestLogging must
// sit outside Recovery so a recovered panic is logged as the 500 response
// it becomes. Recovery is therefore innermost, and a panic raised by
// [ClientIPResolution.Lookup] is not recovered.
//
// Default takes no configuration; every wrapper runs with its zero-value
// defaults. To tune one, register the same set explicitly:
//
//	clientIP := &ClientIPResolution{
//		TrustedCIDRs: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")},
//	}
//	app := NewApp()
//	app.Use(clientIP.Handler, new(RequestLogging).Handler, new(Recovery).Handler)
//
// Each wrapper snapshots its fields when a registration method such as
// [App.Get] applies it, so configure a wrapper before registering routes.
func Default() *App {
	app := NewApp()
	app.Use(
		new(ClientIPResolution).Handler,
		new(RequestLogging).Handler,
		new(Recovery).Handler,
	)
	return app
}

// SetNotFoundHandler sets the handler served when a request matches
// no registered pattern. When unset, the mux responds with 404 Not
// Found.
func (a *App) SetNotFoundHandler(h http.Handler) {
	a.notFoundHandler = h
}

// SetMethodNotAllowedHandler sets the handler served when a request
// path matches a registered pattern but its method does not. When
// unset, the mux responds with 405 Method Not Allowed.
func (a *App) SetMethodNotAllowedHandler(h http.Handler) {
	a.methodNotAllowedHandler = h
}

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
	method, rest := parsePattern(pattern)
	a.handle(method, rest, handler)
}

// HandleFunc registers the handler function for the given pattern,
// with the same behavior as [http.ServeMux.HandleFunc] and [http.HandleFunc].
func (a *App) HandleFunc(pattern string, handlerFunc http.HandlerFunc) {
	method, rest := parsePattern(pattern)
	a.handle(method, rest, handlerFunc)
}

// Any registers handlerFunc for the given path, matching all HTTP methods.
func (a *App) Any(path string, handlerFunc http.HandlerFunc) {
	for _, method := range allMethods {
		a.handle(method, path, handlerFunc)
	}
}

// Get registers handlerFunc for GET requests to the given path.
func (a *App) Get(path string, handlerFunc http.HandlerFunc) {
	a.handle(http.MethodGet, path, handlerFunc)
}

// Post registers handlerFunc for POST requests to the given path.
func (a *App) Post(path string, handlerFunc http.HandlerFunc) {
	a.handle(http.MethodPost, path, handlerFunc)
}

// Delete registers handlerFunc for DELETE requests to the given path.
func (a *App) Delete(path string, handlerFunc http.HandlerFunc) {
	a.handle(http.MethodDelete, path, handlerFunc)
}

// Patch registers handlerFunc for PATCH requests to the given path.
func (a *App) Patch(path string, handlerFunc http.HandlerFunc) {
	a.handle(http.MethodPatch, path, handlerFunc)
}

// Put registers handlerFunc for PUT requests to the given path.
func (a *App) Put(path string, handlerFunc http.HandlerFunc) {
	a.handle(http.MethodPut, path, handlerFunc)
}

// Options registers handlerFunc for OPTIONS requests to the given path.
func (a *App) Options(path string, handlerFunc http.HandlerFunc) {
	a.handle(http.MethodOptions, path, handlerFunc)
}

// Head registers handlerFunc for HEAD requests to the given path.
func (a *App) Head(path string, handlerFunc http.HandlerFunc) {
	a.handle(http.MethodHead, path, handlerFunc)
}

// Connect registers handlerFunc for CONNECT requests to the given path.
func (a *App) Connect(path string, handlerFunc http.HandlerFunc) {
	a.handle(http.MethodConnect, path, handlerFunc)
}

// Trace registers handlerFunc for TRACE requests to the given path.
func (a *App) Trace(path string, handlerFunc http.HandlerFunc) {
	a.handle(http.MethodTrace, path, handlerFunc)
}

// Handler returns the handler and the matching pattern for the given request.
func (a *App) Handler(r *http.Request) (http.Handler, string) {
	return a.mux.Handler(r)
}

// Group creates a new router group with the given relative path
// and invokes fn with it. Routes registered by fn are resolved
// relative to the group's path (see [Router.Group]).
// If fn is nil, Group does nothing.
func (a *App) Group(relativePath string, fn func(r Router)) {
	if fn == nil {
		return
	}
	app := &App{
		basePath:                path.Join(cmp.Or(a.basePath, "/"), relativePath),
		mux:                     a.mux,
		ss:                      slices.Clone(a.ss),
		notFoundHandler:         a.notFoundHandler,
		methodNotAllowedHandler: a.methodNotAllowedHandler,
	}
	fn(app)
}

// handle registers h on the mux, resolving the path against the
// router's base path. An empty method registers h for all
// methods (see [http.ServeMux]).
func (a *App) handle(method, rest string, h http.Handler) {
	pattern := path.Join(a.basePath, rest)
	if strings.HasSuffix(rest, "/") && pattern != "/" {
		pattern += "/"
	}
	if method != "" {
		pattern = method + " " + pattern
	}
	a.mux.Handle(pattern, Chain(a.ss...)(h))
	slog.Debug("route registered", "pattern", pattern)
}

// parsePattern splits s into an optional HTTP method and the path
// pattern, separated by the first space or tab. An empty method
// matches any method. Unlike http's parsePattern, it does not
// validate the pattern.
// See https://github.com/golang/go/blob/master/src/net/http/pattern.go.
func parsePattern(s string) (string, string) {
	method, rest, found := s, "", false
	if i := strings.IndexAny(s, " \t"); i >= 0 {
		method, rest, found = s[:i], strings.TrimLeft(s[i+1:], " \t"), true
	}
	if !found {
		rest = method
		method = ""
	}
	return method, rest
}

// ServeHTTP dispatches r to the handler registered for the matching
// pattern. When nothing matches, it serves the handlers set by
// [App.SetNotFoundHandler] and [App.SetMethodNotAllowedHandler],
// falling back to the mux defaults when unset.
func (a *App) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Fast path: without custom error handlers the mux serves its
	// default 404 and 405 responses directly.
	if a.notFoundHandler == nil && a.methodNotAllowedHandler == nil {
		a.mux.ServeHTTP(w, r)
		return
	}

	h, pattern := a.mux.Handler(r)
	if pattern != "" {
		// Re-dispatch through ServeHTTP instead of serving h: Handler
		// does not populate r.pat/r.matches, so r.Pattern and
		// r.PathValue would be broken for wildcard patterns.
		a.mux.ServeHTTP(w, r)
		return
	}

	// No pattern matched: h is either the mux's 404 handler or its
	// 405 handler (path matched, method did not). Serve it into a
	// recorder to tell the two cases apart.
	rec := &statusRecorder{header: make(http.Header)}
	h.ServeHTTP(rec, r)

	switch rec.code {
	case http.StatusNotFound:
		if a.notFoundHandler != nil {
			a.notFoundHandler.ServeHTTP(w, r)
			return
		}
	case http.StatusMethodNotAllowed:
		if a.methodNotAllowedHandler != nil {
			// RFC 9110 mandates Allow on 405 responses; carry over the
			// methods the mux computed. The custom handler may still
			// override or delete it.
			w.Header().Set("Allow", rec.header.Get("Allow"))
			a.methodNotAllowedHandler.ServeHTTP(w, r)
			return
		}
	}
	// Only the other custom handler is set; replay the mux's default
	// response, including Allow for 405.
	h.ServeHTTP(w, r)
}

// statusRecorder is an [http.ResponseWriter] that discards the body
// and records only the status code and header.
type statusRecorder struct {
	header http.Header
	code   int
}

var _ http.ResponseWriter = (*statusRecorder)(nil)

func (s *statusRecorder) Header() http.Header { return s.header }

func (s *statusRecorder) Write(b []byte) (int, error) { return len(b), nil }

func (s *statusRecorder) WriteHeader(code int) {
	if s.code == 0 {
		s.code = code
	}
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
