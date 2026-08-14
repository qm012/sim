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

// RequestLogger logs each HTTP request via slog.
type RequestLogger struct {
	// LogBytesWritten records the response size.
	LogBytesWritten bool
	// HideQueryString omits the query string from the logged uri,
	// e.g. for tokens or API keys.
	HideQueryString bool
	// ExtraAttrs appends attributes to each record.
	ExtraAttrs func(*http.Request) []slog.Attr
}

// NewRequestLogger returns a new RequestLogger.
func NewRequestLogger() *RequestLogger {
	return &RequestLogger{}
}

// Handler wraps h and logs each request it serves.
// Do not modify the fields after Handler is called.
func (rl *RequestLogger) Handler(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rw := &responseWriter{w: w}
		start := time.Now()

		h.ServeHTTP(rw, r)

		var (
			statusCode = cmp.Or(rw.statusCode, http.StatusOK)
			uri        = r.RequestURI
		)
		if rl.HideQueryString {
			uri = r.URL.Path
		}
		attrs := []slog.Attr{
			slog.Group("request",
				slog.String("pattern", r.Pattern),
				slog.String("method", r.Method),
				slog.String("uri", uri)),
			slog.Int("status_code", statusCode),
			slog.Duration("cost_duration", time.Since(start)),
		}
		if rl.LogBytesWritten {
			attrs = append(attrs, slog.Int("bytes_written", rw.bytesWritten))
		}
		if rl.ExtraAttrs != nil {
			attrs = append(attrs, rl.ExtraAttrs(r)...)
		}

		slogLevel := slog.LevelDebug
		switch {
		case statusCode >= http.StatusInternalServerError: // 5xx
			slogLevel = slog.LevelError
		case statusCode >= http.StatusBadRequest: // 4xx
			slogLevel = slog.LevelWarn
		}
		slog.LogAttrs(r.Context(), slogLevel, "served http request", attrs...)
	})
}
