package main

import (
	"context"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"uuid"

	"github.com/qm012/sim"
)

type ctxTraceIDKey struct{}

func traceIDHandler(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), ctxTraceIDKey{}, uuid.NewV7().String())
		h.ServeHTTP(w, r.WithContext(ctx))
	})
}

func traceIDFromContext(ctx context.Context) string {
	if ip, ok := ctx.Value(ctxTraceIDKey{}).(string); ok {
		return ip
	}
	return ""
}

func panicHandler(_ http.ResponseWriter, _ *http.Request) {
	panic("oops")
}

var pprofLabeling = &sim.PprofLabeling{
	Labels: func(r *http.Request) []string {
		return []string{"trace_id", traceIDFromContext(r.Context()), "pattern", r.Pattern}
	},
}

func init() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	slog.SetDefault(logger)
}

func main() {
	app := sim.NewApp()
	app.Use(
		new(sim.ClientIPResolution).Handler,
		new(sim.RequestLogging).Handler,
		traceIDHandler,
		pprofLabeling.Handler,
		new(sim.Recovery).Handler,
	)
	app.Get("/panic", panicHandler)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := app.Run(ctx, ":8080"); err != nil {
		log.Fatal(err)
	}
}
