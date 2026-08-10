// Copyright (c) 2026 The Sim Authors
// Use of this source code is governed by a MIT license
// that can be found in the LICENSE file.

// Package sim provides an HTTP router built on [net/http.ServeMux],
// extending it with method-based routing helpers.
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

// Router consisting of the core routing methods implemented by App,
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
