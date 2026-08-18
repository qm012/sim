// Copyright (c) 2026 The Sim Authors
// Use of this source code is governed by a MIT license
// that can be found in the LICENSE file.

package sim

import (
	"cmp"
	"log/slog"
	"net/http"
	"time"
)

// RequestLogging logs each HTTP request via slog.
type RequestLogging struct {
	// OmitBytesWritten omits the response body bytes from the logged record.
	OmitBytesWritten bool
	// HideQueryString omits the query string from the logged uri,
	// e.g. for tokens or API keys.
	HideQueryString bool
	// ExtraAttrs appends attributes to each record.
	ExtraAttrs func(*http.Request) []slog.Attr
}

// NewRequestLogging returns a new RequestLogging.
func NewRequestLogging() *RequestLogging {
	return &RequestLogging{}
}

// Handler wraps h and logs each request it serves.
// It captures the current field values at call time;
// later changes do not affect the returned handler.
// If h panics, no record is written for that request; compose this
// handler outside any panic recovery so recovered panics are recorded
// as the error responses they become.
func (rl *RequestLogging) Handler(h http.Handler) http.Handler {
	// Snapshot the configuration; rl is not referenced after this point.
	var (
		omitBytesWritten = rl.OmitBytesWritten
		hideQueryString  = rl.HideQueryString
		extraAttrs       = rl.ExtraAttrs
	)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rw := &responseWriter{w: w}
		start := time.Now()

		h.ServeHTTP(rw, r)

		var (
			statusCode = cmp.Or(rw.statusCode, http.StatusOK)
			uri        = r.RequestURI
		)
		if hideQueryString {
			uri = r.URL.Path
		}
		attrs := []slog.Attr{
			slog.GroupAttrs("request",
				slog.String("pattern", r.Pattern),
				slog.String("method", r.Method),
				slog.String("uri", uri)),
			slog.Int("status_code", statusCode),
			slog.Duration("duration", time.Since(start)),
		}
		if !omitBytesWritten {
			attrs = append(attrs, slog.Int("bytes_written", rw.bytesWritten))
		}
		if extraAttrs != nil {
			attrs = append(attrs, extraAttrs(r)...)
		}

		slogLevel := slog.LevelInfo
		switch {
		case statusCode >= http.StatusInternalServerError: // 5xx
			slogLevel = slog.LevelError
		case statusCode >= http.StatusBadRequest: // 4xx
			slogLevel = slog.LevelWarn
		}
		slog.LogAttrs(r.Context(), slogLevel, "served http request", attrs...)
	})
}
