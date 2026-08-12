// Copyright (c) 2026 The Sim Authors
// Use of this source code is governed by a MIT license
// that can be found in the LICENSE file.

// Package sim provides a small, idiomatic HTTP router built on top of
// [net/http.ServeMux], extending it with method-based routing helpers
// such as [App.Get], [App.Post], and [App.Any].
//
// Example:
//
//	package main
//
//	import (
//		"context"
//		"log/slog"
//		"net/http"
//
//		"github.com/qm012/sim"
//	)
//
//	func main() {
//		app := sim.NewApp()
//
//		app.Get("/", func(w http.ResponseWriter, _ *http.Request) {
//			w.Write([]byte("root."))
//		})
//		app.Get("/users/{id}", func(w http.ResponseWriter, r *http.Request) {
//			w.Write([]byte("user " + r.PathValue("id")))
//		})
//		app.Group("/api", func(r sim.Router) {
//			r.Post("/users", func(w http.ResponseWriter, _ *http.Request) {
//				w.WriteHeader(http.StatusCreated)
//			})
//		})
//
//		if err := app.Run(context.Background(), ":3333"); err != nil {
//			slog.Error("server failed", "err", err)
//		}
//	}
//
// Routes are registered on an [App], which implements [http.Handler]
// and can be passed directly to [http.ListenAndServe] or served with
// [App.Run], which shuts the server down gracefully when its context is
// canceled.
//
// # Patterns
//
// Pattern matching uses the same syntax and precedence rules as
// [http.ServeMux] since Go 1.22. A pattern may carry an optional method
// and host prefix, and a path may contain wildcard segments such as
// {name} and {name...}. Wildcard values are read from the request with
// [http.Request.PathValue]. For example:
//
//   - "GET /users/{id}" matches only GET requests, capturing the id.
//   - "/static/" matches every method and any path under "/static/".
//   - "/files/{path...}" matches the remainder of the URL, including slashes.
//
// The method helpers register the same pattern for a single method:
// [App.Get] registers "GET /path", [App.Post] registers "POST /path",
// and [App.Any] registers "/path" for every method. The pattern given to
// a method helper must be a plain path; method prefixes belong to the
// helper itself.
//
// See the [http.ServeMux] documentation for the complete pattern
// syntax, precedence rules, and trailing-slash redirection behavior.
//
// Wrappers registered with [App.Use] are applied to every handler
// registered after the call, with the first wrapper outermost.
// [Chain] composes wrappers into one; [ChainFunc] is its counterpart
// over http.HandlerFunc, the type accepted by the method helpers such
// as [App.Get].
//
// See the documentation of [App] for the full routing API.
package sim

import (
	"net/http"
)

// allMethods is the complete list of HTTP request methods
// defined by the net/http package.
var allMethods = []string{
	http.MethodGet,
	http.MethodHead,
	http.MethodPost,
	http.MethodPut,
	http.MethodPatch,
	http.MethodDelete,
	http.MethodConnect,
	http.MethodOptions,
	http.MethodTrace,
}

// Router is the set of core routing methods implemented by App,
// using only the standard net/http.
type Router interface {
	// Use registers the given wrappers and applies them to every handler
	// registered after this call. Wrappers run in registration order:
	// the first is outermost and receives the request first, the same
	// composition as [Chain].
	Use(ss ...func(http.Handler) http.Handler)

	// Handle registers the handler for the given pattern, with the same
	// behavior as [http.ServeMux.Handle] and [http.Handle].
	Handle(pattern string, handler http.Handler)
	// HandleFunc registers the handler function for the given pattern,
	// with the same behavior as [http.ServeMux.HandleFunc] and [http.HandleFunc].
	HandleFunc(pattern string, handler http.HandlerFunc)

	// Any Get Post Delete Patch Put Options Head Connect and Trace
	// register handlerFunc on the given pattern for their respective HTTP
	// methods; Any matches all methods.
	Any(path string, handlerFunc http.HandlerFunc)
	Get(path string, handlerFunc http.HandlerFunc)
	Post(path string, handlerFunc http.HandlerFunc)
	Delete(path string, handlerFunc http.HandlerFunc)
	Patch(path string, handlerFunc http.HandlerFunc)
	Put(path string, handlerFunc http.HandlerFunc)
	Options(path string, handlerFunc http.HandlerFunc)
	Head(path string, handlerFunc http.HandlerFunc)
	Connect(path string, handlerFunc http.HandlerFunc)
	Trace(path string, handlerFunc http.HandlerFunc)

	// Group creates a new router group with the given relative path.
	// The fn function registers routes within the group, each of which
	// is resolved relative to the group's path.
	// For example, a group registered at "/api" with a route registered
	// at "/users" handles requests for "/api/users".
	Group(relativePath string, fn func(r Router))
}
