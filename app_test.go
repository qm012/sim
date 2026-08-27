// Copyright (c) 2026 The Sim Authors
// Use of this source code is governed by a MIT license
// that can be found in the LICENSE file.

package sim_test

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/qm012/sim"
)

// markHandler returns an http.Handler that writes mark to the body,
// identifying which handler served a request.
func markHandler(mark string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, mark)
	})
}

// markHandlerFunc returns an http.HandlerFunc with the same behavior as
// markHandler.
func markHandlerFunc(mark string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, mark)
	}
}

// wrapHeader returns a wrapper that adds key: value to the response
// header before delegating to the inner handler.
func wrapHeader(key, value string) func(http.Handler) http.Handler {
	return func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Add(key, value)
			h.ServeHTTP(w, r)
		})
	}
}

func expect(t *testing.T, app *sim.App, method, target, body string) {
	t.Helper()
	rec := serve(t, app, method, target)
	if rec.Code != http.StatusOK || rec.Body.String() != body {
		t.Fatalf("%s %s = %d %q, want 200 %q", method, target, rec.Code, rec.Body.String(), body)
	}
}

func expectStatus(t *testing.T, app *sim.App, method, target string, code int) {
	t.Helper()
	if rec := serve(t, app, method, target); rec.Code != code {
		t.Fatalf("%s %s = %d, want %d", method, target, rec.Code, code)
	}
}

func serve(t *testing.T, h http.Handler, method, target string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), method, target, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestNewApp(t *testing.T) {
	app := sim.NewApp()
	if app == nil {
		t.Fatal("NewApp() returned nil")
	}
}

func TestDefault(t *testing.T) {
	app := sim.Default()
	app.Get("/ok", markHandlerFunc("ok"))
	app.Get("/panic", func(http.ResponseWriter, *http.Request) { panic("boom") })

	t.Run("serves routes", func(t *testing.T) {
		expect(t, app, http.MethodGet, "/ok", "ok")
	})

	t.Run("wrapper composition", func(t *testing.T) {
		var res *httptest.ResponseRecorder
		rec := captureLogs(t, func() {
			res = serve(t, app, http.MethodGet, "/panic")
		})
		if res.Code != http.StatusInternalServerError {
			t.Errorf("GET /panic = %d, want %d", res.Code, http.StatusInternalServerError)
		}
		if got := rec["msg"]; got != "served http request" {
			t.Fatalf("last record msg = %v, want %q", got, "served http request")
		}
		if got := rec["status_code"]; got != float64(http.StatusInternalServerError) {
			t.Errorf("status_code = %v, want %d", got, http.StatusInternalServerError)
		}
		if ip, _ := rec["client_ip"].(string); ip == "" {
			t.Errorf("client_ip = %v, want the peer address", rec["client_ip"])
		}
	})
}

func TestHandleHostPatterns(t *testing.T) {
	app := sim.NewApp()
	app.Handle("api.example.com/", markHandler("host"))
	app.Handle("GET api.example.com/x", markHandler("mhost"))

	expect(t, app, http.MethodGet, "http://api.example.com/x", "mhost")
	expect(t, app, http.MethodPost, "http://api.example.com/x", "host")
	expectStatus(t, app, http.MethodGet, "http://other.com/x", http.StatusNotFound)
}

func TestMethodRouting(t *testing.T) {
	tests := []struct {
		name     string
		method   string
		register func(a *sim.App, path string, h http.HandlerFunc)
	}{
		{http.MethodGet, http.MethodGet, (*sim.App).Get},
		{http.MethodPost, http.MethodPost, (*sim.App).Post},
		{http.MethodDelete, http.MethodDelete, (*sim.App).Delete},
		{http.MethodPatch, http.MethodPatch, (*sim.App).Patch},
		{http.MethodPut, http.MethodPut, (*sim.App).Put},
		{http.MethodOptions, http.MethodOptions, (*sim.App).Options},
		{http.MethodHead, http.MethodHead, (*sim.App).Head},
		{http.MethodConnect, http.MethodConnect, (*sim.App).Connect},
		{http.MethodTrace, http.MethodTrace, (*sim.App).Trace},
	}
	app := sim.NewApp()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.register(app, "/route", markHandlerFunc(tt.name))

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
	app := sim.NewApp()
	app.Get("/page", markHandlerFunc("page"))
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		if rec := serve(t, app, method, "/page"); rec.Code != http.StatusOK {
			t.Errorf("%s /page = %d, want %d", method, rec.Code, http.StatusOK)
		}
	}
	// The reverse is not true: a HEAD-only registration does not serve GET.
	app.Head("/headonly", markHandlerFunc("headonly"))
	if rec := serve(t, app, http.MethodGet, "/headonly"); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET /headonly = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

func TestAllMethodsRegistration(t *testing.T) {
	tests := []struct {
		name     string
		register func(a *sim.App, path string, h http.HandlerFunc)
	}{
		{"Any", (*sim.App).Any},
		{"Handle", func(a *sim.App, path string, h http.HandlerFunc) { a.Handle(path, h) }},
		{"HandleFunc", (*sim.App).HandleFunc},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := sim.NewApp()
			tt.register(app, "/all", markHandlerFunc("all"))
			for _, method := range []string{
				http.MethodGet,
				http.MethodHead,
				http.MethodPost,
				http.MethodPut,
				http.MethodPatch,
				http.MethodDelete,
				http.MethodConnect,
				http.MethodOptions,
				http.MethodTrace,
			} {
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
	app := sim.NewApp()
	app.Get("/users/{id}", func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, "user:%v", r.PathValue("id"))
	})
	rec := serve(t, app, http.MethodGet, "/users/42")
	if rec.Code != http.StatusOK || rec.Body.String() != "user:42" {
		t.Errorf("GET /users/42 = %d %q, want 200 %q", rec.Code, rec.Body.String(), "user:42")
	}
	if rec = serve(t, app, http.MethodGet, "/users/42/posts"); rec.Code != http.StatusNotFound {
		t.Errorf("GET /users/42/posts = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestPrecedenceExactOverWildcard(t *testing.T) {
	app := sim.NewApp()
	app.Get("/users/{id}", markHandlerFunc("wildcard"))
	app.Get("/users/me", markHandlerFunc("exact"))
	if rec := serve(t, app, http.MethodGet, "/users/me"); rec.Body.String() != "exact" {
		t.Errorf("GET /users/me body = %q, want %q", rec.Body.String(), "exact")
	}
	if rec := serve(t, app, http.MethodGet, "/users/42"); rec.Body.String() != "wildcard" {
		t.Errorf("GET /users/42 body = %q, want %q", rec.Body.String(), "wildcard")
	}
}

func TestSubtreePatternAndTrailingSlashRedirect(t *testing.T) {
	app := sim.NewApp()
	app.HandleFunc("/api/", markHandlerFunc("api"))

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

func TestHandlerReturnsMatch(t *testing.T) {
	app := sim.NewApp()
	app.Get("/hello", markHandlerFunc("hello"))

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
	app := sim.NewApp()
	app.Get("/items", markHandlerFunc("items"))

	rec := serve(t, app, http.MethodPost, "/items")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /items = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
	if allow := rec.Header().Get("Allow"); !strings.Contains(allow, http.MethodGet) {
		t.Errorf("Allow = %q, want it to contain %q", allow, http.MethodGet)
	}
}

func handle404405App(notFound, notAllowed http.Handler) *sim.App {
	app := sim.NewApp()
	app.Get("/items", markHandlerFunc("items"))
	app.SetNotFoundHandler(notFound)
	app.SetMethodNotAllowedHandler(notAllowed)
	return app
}

func statusHandler(code int, body string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(code)
		_, _ = io.WriteString(w, body)
	})
}

func TestCustomErrorHandlers(t *testing.T) {
	notFound := statusHandler(http.StatusNotFound, "nf")
	notAllowed := statusHandler(http.StatusMethodNotAllowed, "mna")

	tests := []struct {
		name       string
		notFound   http.Handler
		notAllowed http.Handler
		method     string
		target     string
		wantCode   int
		wantBody   string
	}{
		{
			name:     "custom not found",
			notFound: notFound,
			method:   http.MethodGet,
			target:   "/nope",
			wantCode: http.StatusNotFound,
			wantBody: "nf",
		},
		{
			name:       "custom method not allowed",
			notAllowed: notAllowed,
			method:     http.MethodPost,
			target:     "/items",
			wantCode:   http.StatusMethodNotAllowed,
			wantBody:   "mna",
		},
		{
			name:       "default not found when only 405 handler set",
			notAllowed: notAllowed,
			method:     http.MethodGet,
			target:     "/nope",
			wantCode:   http.StatusNotFound,
			wantBody:   "404 page not found\n",
		},
		{
			name:     "default method not allowed when only 404 handler set",
			notFound: notFound,
			method:   http.MethodPost,
			target:   "/items",
			wantCode: http.StatusMethodNotAllowed,
			wantBody: "Method Not Allowed\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := serve(t, handle404405App(tt.notFound, tt.notAllowed), tt.method, tt.target)
			if rec.Code != tt.wantCode {
				t.Fatalf("%s %s = %d, want %d", tt.method, tt.target, rec.Code, tt.wantCode)
			}
			if rec.Body.String() != tt.wantBody {
				t.Errorf("%s %s body = %q, want %q", tt.method, tt.target, rec.Body.String(), tt.wantBody)
			}
			if tt.wantCode == http.StatusMethodNotAllowed &&
				!strings.Contains(rec.Header().Get("Allow"), http.MethodGet) {
				t.Errorf("Allow = %q, want it to contain %q", rec.Header().Get("Allow"), http.MethodGet)
			}
		})
	}
}

func TestCustomErrorHandlersBothSet(t *testing.T) {
	app := handle404405App(statusHandler(http.StatusNotFound, "nf"), statusHandler(http.StatusMethodNotAllowed, "mna"))

	if rec := serve(t, app, http.MethodGet, "/nope"); rec.Code != http.StatusNotFound || rec.Body.String() != "nf" {
		t.Errorf("GET /nope = %d %q, want %d %q", rec.Code, rec.Body.String(), http.StatusNotFound, "nf")
	}
	rec := serve(t, app, http.MethodPost, "/items")
	if rec.Code != http.StatusMethodNotAllowed || rec.Body.String() != "mna" {
		t.Errorf("POST /items = %d %q, want %d %q", rec.Code, rec.Body.String(), http.StatusMethodNotAllowed, "mna")
	}
	if allow := rec.Header().Get("Allow"); !strings.Contains(allow, http.MethodGet) {
		t.Errorf("Allow = %q, want it to contain %q", allow, http.MethodGet)
	}
}

// Matched routes and redirects must be unaffected by the custom
// error handlers.
func TestCustomErrorHandlersDoNotAffectRouting(t *testing.T) {
	app := handle404405App(statusHandler(http.StatusNotFound, "nf"), statusHandler(http.StatusMethodNotAllowed, "mna"))
	app.Get("/tree/", markHandlerFunc("tree"))

	expect(t, app, http.MethodGet, "/items", "items")
	expect(t, app, http.MethodGet, "/tree/", "tree")

	rec := serve(t, app, http.MethodGet, "/tree")
	if rec.Code != http.StatusTemporaryRedirect {
		t.Fatalf("GET /tree = %d, want %d", rec.Code, http.StatusTemporaryRedirect)
	}
	if loc := rec.Header().Get("Location"); loc != "/tree/" {
		t.Errorf("Location = %q, want %q", loc, "/tree/")
	}
}

func TestConflictingPatternPanics(t *testing.T) {
	app := sim.NewApp()
	app.Get("/a/{x}", markHandlerFunc("first"))
	defer func() {
		if recover() == nil {
			t.Error("Handle did not panic on a conflicting pattern")
		}
	}()
	// "/a/b" conflicts with "GET /a/{x}": each matches a request the other does not.
	app.Handle("/a/b", markHandler("second"))
}

func TestGroupDuplicateRegistrationPanics(t *testing.T) {
	app := sim.NewApp()
	defer func() {
		if recover() == nil {
			t.Error("duplicate route in a group did not panic")
		}
	}()
	app.Group("/api", func(r sim.Router) {
		r.Get("/x", markHandlerFunc("first"))
		r.Get("/x", markHandlerFunc("second"))
	})
}

func TestGroupConflictingRoutesAcrossGroups(t *testing.T) {
	app := sim.NewApp()
	app.Group("/a", func(r sim.Router) { r.Get("/x", markHandlerFunc("first")) })
	defer func() {
		if recover() == nil {
			t.Error("conflicting routes across groups did not panic")
		}
	}()
	app.Group("/a", func(r sim.Router) { r.Get("/x", markHandlerFunc("second")) })
}

func TestUseAppliesToSubsequentHandlers(t *testing.T) {
	app := sim.NewApp()
	var r sim.Router = app

	// Registered before Use: not wrapped.
	r.Get("/pre", markHandlerFunc("pre"))
	r.Use(wrapHeader("X-Wrapped", "yes"))
	r.Get("/post", markHandlerFunc("post"))

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
	app := sim.NewApp()
	app.Use(wrapHeader("X-Order", "a"))
	app.Use(wrapHeader("X-Order", "b"))

	app.Get("/", markHandlerFunc("h"))
	rec := serve(t, app, http.MethodGet, "/")
	if got := strings.Join(rec.Header().Values("X-Order"), ""); got != "ab" || rec.Body.String() != "h" {
		t.Errorf("wrapper order = %q, want %q", got, "ab")
	}
}

func TestUseEmptyIsNoop(t *testing.T) {
	app := sim.NewApp()
	app.Use()
	app.Get("/", markHandlerFunc("h"))
	expect(t, app, http.MethodGet, "/", "h")
}

func TestUseAppliesToAnyAndHandle(t *testing.T) {
	app := sim.NewApp()
	app.Use(wrapHeader("X-Wrapped", "yes"))
	app.Any("/any", markHandlerFunc("any"))
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
	app := sim.NewApp()
	app.Use(func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Header.Set("X-Wrapped", "yes")
			h.ServeHTTP(w, r)
		})
	})
	app.Get("/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, r.Header.Get("X-Wrapped"))
	})
	expect(t, app, http.MethodGet, "/", "yes")
}

func TestChainWrappedHandlerRegistration(t *testing.T) {
	chainWrapped := sim.Chain(
		wrapHeader("X-Order", "a"),
		wrapHeader("X-Order", "b"),
	)
	chainFuncWrapped := sim.ChainFunc(
		wrapHeader("X-Order", "1"),
		wrapHeader("X-Order", "2"),
	)

	tests := []struct {
		name       string
		path       string
		method     string
		wantValues string
		register   func(a *sim.App, path string)
	}{
		{"Get", "/get", http.MethodGet, "12", func(a *sim.App, path string) {
			a.Get(path, chainFuncWrapped(markHandlerFunc(path)))
		}},
		{"Post", "/post", http.MethodPost, "12", func(a *sim.App, path string) {
			a.Post(path, chainFuncWrapped(markHandlerFunc(path)))
		}},
		{"Put", "/put", http.MethodPut, "12", func(a *sim.App, path string) {
			putFunc := func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(w, path)
			}
			a.Put(path, chainFuncWrapped(putFunc))
		}},
		{"Handle", "/handle", http.MethodGet, "ab", func(a *sim.App, path string) {
			a.Handle(path, chainWrapped(markHandler(path)))
		}},
		{"HandleFunc", "/handlefunc", http.MethodGet, "12", func(a *sim.App, path string) {
			a.HandleFunc(path, chainFuncWrapped(markHandlerFunc(path)))
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := sim.NewApp()
			tt.register(app, tt.path)
			rec := serve(t, app, tt.method, tt.path)
			if got := strings.Join(rec.Header().Values("X-Order"), ""); got != tt.wantValues {
				t.Errorf("%s X-Order = %q, want %q", tt.name, got, tt.wantValues)
			}
			if got := rec.Body.String(); got != tt.path {
				t.Errorf("%s body = %q, want %q", tt.name, got, tt.path)
			}
		})
	}
}

func TestRunInvalidAddr(t *testing.T) {
	app := sim.NewApp()
	if err := app.Run(t.Context(), "localhost:99999"); err == nil {
		t.Error("Run with an invalid port returned nil error")
	}
}

//nolint:funlen // registers a route tree then asserts every branch
func TestGroupRouting(t *testing.T) {
	app := sim.NewApp()
	app.Group("/api", func(r sim.Router) {
		r.Get("/users", markHandlerFunc("users"))
		r.HandleFunc("/func", markHandlerFunc("func"))
		r.Handle("GET /handle", markHandler("handle"))
		r.Group("/v1", func(r sim.Router) {
			r.Get("/x", markHandlerFunc("nv1x"))
			r.Group("/c", func(r sim.Router) { r.Get("/deep", markHandlerFunc("deep")) })
		})
		r.Group("", func(r sim.Router) {
			r.Get("/y", markHandlerFunc("ny"))
			r.Get("xbb", markHandlerFunc("xbb"))
			r.Get("xcc//", markHandlerFunc("xcc"))
		})
		r.Group("/", func(r sim.Router) {
			r.Get("/z", markHandlerFunc("nz"))
		})
		r.Group("", func(r sim.Router) {
			r.Group("/v2", func(r sim.Router) {
				r.Get("/t", markHandlerFunc("nv2t"))
			})
		})
	})
	app.Group("api", func(r sim.Router) {
		r.Get("/items", markHandlerFunc("items"))
		r.Get("ubc", markHandlerFunc("ubc"))
	})
	app.Group("/api/", func(r sim.Router) {
		r.Get("/orders", markHandlerFunc("orders"))
		r.Get("xorders", markHandlerFunc("xorders"))
		r.Get("//morders", markHandlerFunc("morders"))
		r.Get("//mdorders///", markHandlerFunc("mdorders"))
		r.Get("///d//mdorders//b//cc/", markHandlerFunc("mutilmdorders"))
	})
	app.Group("", func(r sim.Router) {
		r.Get("/ABC", markHandlerFunc("ABC"))
		r.Get("ccc", markHandlerFunc("ccc"))
	})
	app.Group("/", func(r sim.Router) {
		r.Get("/DEF", markHandlerFunc("def"))
		r.Get("abd", markHandlerFunc("abd"))
		r.Group("/", func(r sim.Router) {
			r.Get("/abc", markHandlerFunc("abc"))
		})
	})
	app.Group("/nil", nil)

	expect(t, app, http.MethodGet, "/api/users", "users")
	expect(t, app, http.MethodGet, "/api/items", "items")
	expect(t, app, http.MethodGet, "/api/ubc", "ubc")
	expect(t, app, http.MethodGet, "/api/orders", "orders")
	expect(t, app, http.MethodGet, "/api/xorders", "xorders")
	expect(t, app, http.MethodGet, "/api/morders", "morders")
	expect(t, app, http.MethodGet, "/api/mdorders/", "mdorders")
	expect(t, app, http.MethodGet, "/api/d/mdorders/b/cc/", "mutilmdorders")
	expect(t, app, http.MethodGet, "/ABC", "ABC")
	expect(t, app, http.MethodGet, "/ccc", "ccc")
	expect(t, app, http.MethodGet, "/DEF", "def")
	expect(t, app, http.MethodGet, "/abd", "abd")
	expect(t, app, http.MethodGet, "/abc", "abc")
	expectStatus(t, app, http.MethodGet, "/users", http.StatusNotFound)
	expect(t, app, http.MethodGet, "/api/func", "func")
	expect(t, app, http.MethodPost, "/api/func", "func")
	expect(t, app, http.MethodGet, "/api/handle", "handle")
	expectStatus(t, app, http.MethodPost, "/api/handle", http.StatusMethodNotAllowed)
	expect(t, app, http.MethodGet, "/api/v1/x", "nv1x")
	expect(t, app, http.MethodGet, "/api/v1/c/deep", "deep")
	expect(t, app, http.MethodGet, "/api/y", "ny")
	expect(t, app, http.MethodGet, "/api/xbb", "xbb")
	expect(t, app, http.MethodGet, "/api/xcc/", "xcc")
	expect(t, app, http.MethodGet, "/api/z", "nz")
	expect(t, app, http.MethodGet, "/api/v2/t", "nv2t")
	expectStatus(t, app, http.MethodPost, "/nil", http.StatusNotFound)
}

func TestGroupPatternMatching(t *testing.T) {
	app := sim.NewApp()
	app.Group("/api/{v}", func(r sim.Router) {
		r.Get("/", markHandlerFunc("vroot"))
		r.Get("/{id}", markHandlerFunc("vid"))
		r.Get("/x", markHandlerFunc("vx"))
	})
	app.Group("/pat", func(r sim.Router) {
		r.Get("/{id}", markHandlerFunc("wild"))
	})
	app.Group("/multi/x", func(r sim.Router) {
		r.Get("/{rest...}", markHandlerFunc("rest"))
	})
	app.Group("/stat", func(r sim.Router) {
		r.HandleFunc("/files/", markHandlerFunc("static"))
	})
	app.Group("/api", func(r sim.Router) {
		r.Get("/{$}", markHandlerFunc("apir"))
	})
	app.Group("/public", func(r sim.Router) {
		r.Get("/", markHandlerFunc("pub"))
		r.Get("", markHandlerFunc("exact"))
	})
	app.Group("/p", func(r sim.Router) { r.Handle("PATCH \t /", markHandler("patch")) })

	app.Group("/sib1", func(r sim.Router) { r.Get("/x", markHandlerFunc("sib1")) })
	app.Group("/sib2", func(r sim.Router) { r.Get("/x", markHandlerFunc("sib2")) })

	expect(t, app, http.MethodGet, "/api/v1/", "vroot")
	expectStatus(t, app, http.MethodGet, "/api/v1", http.StatusTemporaryRedirect)
	// /api/x has no exact match; only its trailing-slash form /api/x/
	// matches the {v}/ subtree, so ServeMux 307-redirects.
	expectStatus(t, app, http.MethodGet, "/api/x", http.StatusTemporaryRedirect)
	expect(t, app, http.MethodGet, "/api/v1/123", "vid")
	expect(t, app, http.MethodGet, "/api/v1/x", "vx")
	expect(t, app, http.MethodGet, "/pat/123", "wild")
	expect(t, app, http.MethodGet, "/multi/x/a/b/c", "rest")
	expect(t, app, http.MethodGet, "/stat/files/x", "static")

	expect(t, app, http.MethodGet, "/public", "exact")
	expect(t, app, http.MethodGet, "/public/", "pub")
	// {$} restricts the route to exactly /api/: /api/ serves it, and
	// /api is 307-redirected to it.
	expectStatus(t, app, http.MethodGet, "/api", http.StatusTemporaryRedirect)
	expect(t, app, http.MethodGet, "/api/", "apir")

	expect(t, app, http.MethodPatch, "/p/anything", "patch")
	expectStatus(t, app, http.MethodGet, "/p/anything", http.StatusMethodNotAllowed)

	expect(t, app, http.MethodGet, "/sib1/x", "sib1")
	expect(t, app, http.MethodGet, "/sib2/x", "sib2")
}

func TestGroupWrapperScoping(t *testing.T) {
	app := sim.NewApp()

	app.Get("/root", markHandlerFunc("root"))
	app.Use(wrapHeader("X-Wrapped", "root"))
	app.Group("/wrapped", func(r sim.Router) {
		r.Use(wrapHeader("X-Group", "g"))
		r.Get("/g", markHandlerFunc("g"))
	})
	app.Group("/nested", func(r sim.Router) {
		r.Use(wrapHeader("X-Order", "outer"))
		r.Group("/v1", func(r sim.Router) {
			r.Use(wrapHeader("X-Order", "inner"))
			r.Get("/x", markHandlerFunc("x"))
		})
	})
	app.Group("", func(r sim.Router) {
		r.Use(wrapHeader("X-Group", "1"))
		r.Get("/g1", markHandlerFunc("g1"))
	})
	app.Group("", func(r sim.Router) {
		r.Use(wrapHeader("X-Group", "2"))
		r.Get("/g2", markHandlerFunc("g2"))
	})

	if rec := serve(t, app, http.MethodGet, "/root"); rec.Code != http.StatusOK ||
		rec.Body.String() != "root" || rec.Header().Get("X-Wrapped") != "" || rec.Header().Get("X-Group") != "" {
		t.Fatalf("GET /root = %d, X-Wrapped %q, X-Group %q, body %q; want unwrapped %q",
			rec.Code, rec.Header().Get("X-Wrapped"), rec.Header().Get("X-Group"), rec.Body.String(), "root")
	}
	if rec := serve(t, app, http.MethodGet, "/wrapped/g"); rec.Code != http.StatusOK ||
		rec.Header().Get("X-Wrapped") != "root" || rec.Header().Get("X-Group") != "g" {
		t.Fatalf("GET /wrapped/g = %d, X-Wrapped %q, X-Group %q; want %q and %q",
			rec.Code, rec.Header().Get("X-Wrapped"), rec.Header().Get("X-Group"), "root", "g")
	}
	if got := strings.Join(
		serve(t, app, http.MethodGet, "/nested/v1/x").Header().Values("X-Order"), ""); got != "outerinner" {
		t.Fatalf("nested Use order = %q, want %q", got, "outerinner")
	}
	if rec := serve(t, app, http.MethodGet, "/g1"); rec.Code != http.StatusOK || rec.Header().Get("X-Group") != "1" {
		t.Fatalf("GET /g1 = %d, X-Group %q; want %q", rec.Code, rec.Header().Get("X-Group"), "1")
	}
	if rec := serve(t, app, http.MethodGet, "/g2"); rec.Code != http.StatusOK || rec.Header().Get("X-Group") != "2" {
		t.Fatalf("GET /g2 = %d, X-Group %q; want %q", rec.Code, rec.Header().Get("X-Group"), "2")
	}
}
