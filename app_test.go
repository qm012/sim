// Copyright (c) 2026 qm012<1007661792@qq.com>. All rights reserved.
// Use of this source code is governed by a MIT license
// that can be found in the LICENSE file.

package sim

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// markHandler returns a handler that writes mark as the response body,
// used to verify which registered handler served a request.
func markHandler(mark string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, mark)
	}
}

// wrapHeader returns a wrapper that adds key: value to the response
// header before delegating to the inner handler, like real middlewares
// that mark their work on the response.
func wrapHeader(key, value string) func(http.Handler) http.Handler {
	return func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Add(key, value)
			h.ServeHTTP(w, r)
		})
	}
}

// serve performs an HTTP request against app and returns the recorder.
func serve(t *testing.T, h http.Handler, method, target string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), method, target, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestNewApp(t *testing.T) {
	app := NewApp()
	if app == nil {
		t.Fatal("NewApp() returned nil")
	}
	if app.mux == nil {
		t.Fatal("app.mux returned nil")
	}
}

func TestMethodRouting(t *testing.T) {
	tests := []struct {
		name     string
		method   string
		register func(a *App, path string, h http.HandlerFunc)
	}{
		{http.MethodGet, http.MethodGet, (*App).Get},
		{http.MethodPost, http.MethodPost, (*App).Post},
		{http.MethodDelete, http.MethodDelete, (*App).Delete},
		{http.MethodPatch, http.MethodPatch, (*App).Patch},
		{http.MethodPut, http.MethodPut, (*App).Put},
		{http.MethodOptions, http.MethodOptions, (*App).Options},
		{http.MethodHead, http.MethodHead, (*App).Head},
		{http.MethodConnect, http.MethodConnect, (*App).Connect},
		{http.MethodTrace, http.MethodTrace, (*App).Trace},
	}
	app := NewApp()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.register(app, "/route", markHandler(tt.name))

			// The registered method is served by its own handler.
			rec := serve(t, app, tt.method, "/route")
			if rec.Code != http.StatusOK {
				t.Fatalf("%s /route = %d, want %d", tt.method, rec.Code, http.StatusOK)
			}
			if got := rec.Body.String(); got != tt.name {
				t.Errorf("%s /route body = %q, want %q", tt.method, got, tt.name)
			}
		})
	}
}

func TestGetPatternAlsoMatchesHead(t *testing.T) {
	app := NewApp()
	app.Get("/page", markHandler("page"))
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		if rec := serve(t, app, method, "/page"); rec.Code != http.StatusOK {
			t.Errorf("%s /page = %d, want %d", method, rec.Code, http.StatusOK)
		}
	}
	// The reverse is not true: a HEAD-only registration does not serve GET.
	app.Head("/headonly", markHandler("headonly"))
	if rec := serve(t, app, http.MethodGet, "/headonly"); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET /headonly = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

// TestAllMethodsRegistration verifies that handlers registered via Any,
// Handle, and HandleFunc are served for every HTTP method.
func TestAllMethodsRegistration(t *testing.T) {
	tests := []struct {
		name     string
		register func(a *App, path string, h http.HandlerFunc)
	}{
		{"Any", (*App).Any},
		{"Handle", func(a *App, path string, h http.HandlerFunc) { a.Handle(path, h) }},
		{"HandleFunc", (*App).HandleFunc},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := NewApp()
			tt.register(app, "/all", markHandler("all"))
			for _, method := range allMethods {
				rec := serve(t, app, method, "/all")
				if rec.Code != http.StatusOK {
					t.Errorf("%s /all = %d, want %d", method, rec.Code, http.StatusOK)
				}
				if got := rec.Body.String(); got != "all" {
					t.Errorf("%s /all body = %q, want %q", method, got, "all")
				}
			}
		})
	}
}

func TestWildcardPathValue(t *testing.T) {
	app := NewApp()
	app.Get("/users/{id}", func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, "user:%v", r.PathValue("id"))
	})
	rec := serve(t, app, http.MethodGet, "/users/42")
	if rec.Code != http.StatusOK || rec.Body.String() != "user:42" {
		t.Errorf("GET /users/42 = %d %q, want 200 %q", rec.Code, rec.Body.String(), "user:42")
	}
	// {id} matches a single path segment only.
	if rec := serve(t, app, http.MethodGet, "/users/42/posts"); rec.Code != http.StatusNotFound {
		t.Errorf("GET /users/42/posts = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestPrecedenceExactOverWildcard(t *testing.T) {
	app := NewApp()
	app.Get("/users/{id}", markHandler("wildcard"))
	app.Get("/users/me", markHandler("exact"))
	if rec := serve(t, app, http.MethodGet, "/users/me"); rec.Body.String() != "exact" {
		t.Errorf("GET /users/me body = %q, want %q", rec.Body.String(), "exact")
	}
	if rec := serve(t, app, http.MethodGet, "/users/42"); rec.Body.String() != "wildcard" {
		t.Errorf("GET /users/42 body = %q, want %q", rec.Body.String(), "wildcard")
	}
}

func TestSubtreePatternAndTrailingSlashRedirect(t *testing.T) {
	app := NewApp()
	app.HandleFunc("/api/", markHandler("api"))

	// A subtree pattern matches any path beneath it, for any method.
	if rec := serve(t, app, http.MethodPost, "/api/v1/items"); rec.Code != http.StatusOK || rec.Body.String() != "api" {
		t.Errorf("POST /api/v1/items = %d %q, want 200 %q", rec.Code, rec.Body.String(), "api")
	}

	// Requesting the subtree root without a trailing slash redirects to it
	// (Go 1.22+ ServeMux uses a 307 Temporary Redirect).
	rec := serve(t, app, http.MethodGet, "/api")
	if rec.Code != http.StatusTemporaryRedirect {
		t.Errorf("GET /api = %d, want %d", rec.Code, http.StatusTemporaryRedirect)
	}
	if loc := rec.Header().Get("Location"); loc != "/api/" {
		t.Errorf("Location = %q, want %q", loc, "/api/")
	}
}

func TestHandler(t *testing.T) {
	app := NewApp()
	app.Get("/hello", markHandler("hello"))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/hello", nil)
	h, pattern := app.Handler(req)
	if pattern != "GET /hello" {
		t.Errorf("pattern = %q, want %q", pattern, "GET /hello")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Body.String() != "hello" {
		t.Errorf("handler body = %q, want %q", rec.Body.String(), "hello")
	}

	// An unmatched path yields the NotFound handler and an empty pattern.
	req = httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/nope", nil)
	h, pattern = app.Handler(req)
	if pattern != "" {
		t.Errorf("pattern = %q, want empty", pattern)
	}
	if h == nil {
		t.Fatal("Handler returned nil handler for an unmatched path")
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("unmatched handler status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestMethodNotAllowed(t *testing.T) {
	app := NewApp()
	app.Get("/items", markHandler("items"))
	rec := serve(t, app, http.MethodPost, "/items")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /items = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
	if allow := rec.Header().Get("Allow"); !strings.Contains(allow, http.MethodGet) {
		t.Errorf("Allow = %q, want it to contain %q", allow, http.MethodGet)
	}
}

func TestConflictingPatternPanics(t *testing.T) {
	app := NewApp()
	app.Get("/a/{x}", markHandler("first"))
	defer func() {
		if recover() == nil {
			t.Error("Handle did not panic on a conflicting pattern")
		}
	}()
	// "/a/b" conflicts with "GET /a/{x}": each matches a request the other does not.
	app.Handle("/a/b", markHandler("second"))
}

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

func TestChainEmpty(t *testing.T) {
	h := markHandler("h")
	rec := serve(t, Chain()(h), http.MethodGet, "/")
	if got := rec.Body.String(); got != "h" {
		t.Errorf("Chain()(h) body = %q, want %q", got, "h")
	}
}

func TestUseAppliesToSubsequentHandlers(t *testing.T) {
	app := NewApp()
	var r Router = app

	// Registered before Use: not wrapped.
	r.Get("/pre", markHandler("pre"))
	r.Use(wrapHeader("X-Wrapped", "yes"))
	r.Get("/post", markHandler("post"))

	rec := serve(t, app, http.MethodGet, "/pre")
	if got := rec.Header().Get("X-Wrapped"); got != "" {
		t.Errorf("handler registered before Use was wrapped: got %q, want empty", got)
	}
	if got := rec.Body.String(); got != "pre" {
		t.Errorf("GET /pre body = %q, want %q", got, "pre")
	}
	rec = serve(t, app, http.MethodGet, "/post")
	if got := rec.Header().Get("X-Wrapped"); got != "yes" {
		t.Errorf("handler registered after Use was not wrapped: got %q, want %q", got, "yes")
	}
	if got := rec.Body.String(); got != "post" {
		t.Errorf("GET /post body = %q, want %q", got, "post")
	}
}

func TestUseAccumulatesInOrder(t *testing.T) {
	app := NewApp()
	app.Use(wrapHeader("X-Order", "a"))
	app.Use(wrapHeader("X-Order", "b"))
	app.Get("/", markHandler("h"))
	// The first Use call is outermost, so it records its name first.
	if got := strings.Join(
		serve(t, app, http.MethodGet, "/").Header().Values("X-Order"),
		""); got != "ab" {
		t.Errorf("wrapper order = %q, want %q", got, "ab")
	}
}

func TestUseEmptyIsNoop(t *testing.T) {
	app := NewApp()
	app.Use()
	app.Get("/", markHandler("h"))
	if got := serve(t, app, http.MethodGet, "/").Body.String(); got != "h" {
		t.Errorf("body = %q, want %q", got, "h")
	}
}

func TestUseAppliesToAnyAndHandle(t *testing.T) {
	app := NewApp()
	app.Use(wrapHeader("X-Wrapped", "yes"))
	app.Any("/any", markHandler("any"))
	app.Handle("/all", markHandler("all"))
	for _, tt := range []struct{ method, path string }{
		{http.MethodGet, "/any"},
		{http.MethodPost, "/all"},
	} {
		rec := serve(t, app, tt.method, tt.path)
		if got := rec.Header().Get("X-Wrapped"); got != "yes" {
			t.Errorf("%s %s was not wrapped: got %q, want %q", tt.method, tt.path, got, "yes")
		}
	}
}

func TestUseWrapperSeesRequestFirst(t *testing.T) {
	app := NewApp()
	app.Use(func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Header.Set("X-Wrapped", "yes")
			h.ServeHTTP(w, r)
		})
	})
	app.Get("/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, r.Header.Get("X-Wrapped"))
	})
	if got := serve(t, app, http.MethodGet, "/").Body.String(); got != "yes" {
		t.Errorf("handler did not see the header set by the wrapper: got %q, want %q", got, "yes")
	}
}

func TestRunInvalidAddr(t *testing.T) {
	app := NewApp()
	if err := app.Run(t.Context(), "localhost:99999"); err == nil {
		t.Error("Run with an invalid port returned nil error")
	}
}
