// Copyright (c) 2026 qm012<1007661792@qq.com>. All rights reserved.
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
