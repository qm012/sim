package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"net/http/pprof"
	"os"
	"os/signal"
	"syscall"

	"github.com/google/uuid" //nolint:depguard // TODO: use the built-in `uuid` after upgrading to Go 1.27

	"github.com/qm012/sim"
)

// init installs a JSON logger that decorates every record with the
// per-request trace_id (see traceIDLogHandler).
func init() {
	logger := slog.New(newTraceIDLogHandler(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug})))
	slog.SetDefault(logger)
}

func main() {
	app := sim.NewApp()
	app.Use(
		new(sim.ClientIPResolution).Handler,
		traceIDHandler,
		new(sim.PprofLabeling).Handler,
		new(sim.RequestLogging).Handler,
		new(sim.Recovery).Handler,
	)
	app.Get("/panic", panicHandler)
	app.Get("/hello", helloHandler)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	go metricsServer(ctx, ":8081")
	if err := app.Run(ctx, ":8080"); err != nil {
		log.Fatal(err)
	}
}

// metricsServer serves the net/http/pprof handlers on a dedicated
// port, kept off the app's mux so profiling endpoints are not
// exposed on the public listener. pprof.Index additionally serves
// the goroutine, threadcreate, heap, allocs, block, mutex and
// goroutineleak profiles without any extra registration.
func metricsServer(ctx context.Context, addr string) {
	app := sim.NewApp()
	app.Get("/debug/pprof/", pprof.Index)
	app.Get("/debug/pprof/cmdline", pprof.Cmdline)
	app.Get("/debug/pprof/profile", pprof.Profile)
	app.Get("/debug/pprof/symbol", pprof.Symbol)
	app.Get("/debug/pprof/trace", pprof.Trace)
	if err := app.Run(ctx, addr); err != nil {
		log.Fatal(err)
	}
}

type ctxTraceIDKey struct{}

func traceIDHandler(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), ctxTraceIDKey{}, uuid.New().String())
		h.ServeHTTP(w, r.WithContext(ctx))
	})
}

func traceIDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(ctxTraceIDKey{}).(string); ok {
		return id
	}
	return ""
}

func helloHandler(w http.ResponseWriter, _ *http.Request) {
	_, _ = fmt.Fprintf(w, "hello")
}

func panicHandler(_ http.ResponseWriter, _ *http.Request) {
	panic("oops")
}
