// Copyright (c) 2026 The Sim Authors
// Use of this source code is governed by a MIT license
// that can be found in the LICENSE file.

package sim

import (
	"net/http"
	"slices"
)

// Chain returns a function that composes the given wrappers into a single
// wrapper. Applying the returned function to a handler h returns a new
// handler that runs each wrapper in order: ss[0] is outermost,
// receives the request first, and its response is what the caller ultimately
// sees.
//
// Chain(Logging, Auth)(h) is equivalent to Logging(Auth(h)).
// With no wrappers, Chain returns a function that leaves its argument
// unchanged.
func Chain(ss ...func(http.Handler) http.Handler) func(http.Handler) http.Handler {
	ss = slices.Clone(ss)
	return func(h http.Handler) http.Handler {
		for _, s := range slices.Backward(ss) {
			h = s(h)
		}
		return h
	}
}

// ChainFunc returns a function that composes the given wrappers into a
// single wrapper over http.HandlerFunc handlers, the counterpart of Chain
// for func-typed registration methods such as [App.Get] and [App.Put].
//
// The wrappers are the same func(http.Handler) http.Handler middleware
// as Chain's, so middleware written for Chain works unchanged.
//
// With no wrappers, ChainFunc returns a function that leaves its argument
// unchanged.
func ChainFunc(ss ...func(http.Handler) http.Handler) func(http.HandlerFunc) http.HandlerFunc {
	ss = slices.Clone(ss) // same defensive copy as Chain
	return func(h http.HandlerFunc) http.HandlerFunc {
		return Chain(ss...)(h).ServeHTTP
	}
}
