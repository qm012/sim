package main

import (
	"context"
	"errors"
	"log/slog"
)

// traceIDLogHandler is a [slog.Handler] that attaches the trace ID
// stored in the request context to each log record as trace_id.
type traceIDLogHandler struct {
	next slog.Handler
}

var _ slog.Handler = (*traceIDLogHandler)(nil)

// newTraceIDLogHandler returns a handler that decorates next with a
// trace_id attribute sourced from the request context.
func newTraceIDLogHandler(next slog.Handler) slog.Handler {
	return &traceIDLogHandler{
		next: next,
	}
}

// Enabled implements [slog.Handler].
func (h *traceIDLogHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

// Handle implements [slog.Handler].
func (h *traceIDLogHandler) Handle(ctx context.Context, record slog.Record) error {
	if h.next == nil {
		return errors.New("traceIDLog: handler is missing")
	}

	record.AddAttrs(slog.String("trace_id", traceIDFromContext(ctx)))
	return h.next.Handle(ctx, record)
}

// WithAttrs implements [slog.Handler].
func (h *traceIDLogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if h.next == nil {
		return h
	}
	return &traceIDLogHandler{next: h.next.WithAttrs(attrs)}
}

// WithGroup implements [slog.Handler].
func (h *traceIDLogHandler) WithGroup(name string) slog.Handler {
	if h.next == nil {
		return h
	}
	return &traceIDLogHandler{next: h.next.WithGroup(name)}
}
