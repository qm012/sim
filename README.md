# Sim Web Framework

Sim is a minimal HTTP web framework for Go, built on top of `net/http`
and `http.ServeMux` — no third-party dependencies.

## Features

- Zero dependencies: only the Go standard library
- Method-based routing: `Get`, `Post`, `Put`, `Delete`, `Patch`,
  `Options`, `Head`, `Connect`, `Trace`, and `Any`
- Routing follows the [net/http.ServeMux](https://pkg.go.dev/net/http#ServeMux) patterns
- Standard `net/http` handlers and wrappers
- Wrapper composition with `Chain` and `ChainFunc`
- Route groups
- Client IP resolution: `ClientIPResolution` wrapper with trusted-proxy
  support that populates `client_ip` in request logs
- Request logging: `RequestLogging` wrapper writing structured `slog` records
- Panic recovery: `Recovery` wrapper logging a stack trace and writing a 500
- `Default` bundles those three wrappers, ready to use with no configuration
- Graceful shutdown with `Run`

## Installation

Requires Go 1.26+.

```bash
go get github.com/qm012/sim
```

## Example

A complete runnable REST API:

```go
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/qm012/sim"
)

func main() {
	// Default registers three wrappers, outermost first:
	// ClientIPResolution, RequestLogging, Recovery.
	app := sim.Default()

	app.Get("/", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("welcome"))
	})
	app.Any("/ping", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("pong"))
	})

	// Group routes under a common prefix.
	app.Group("/api", func(r sim.Router) {
		r.Get("/users", listUsers)
		r.Get("/users/{id}", getUser)
		r.Post("/users", createUser)
		r.Put("/users/{id}", updateUser)
		r.Delete("/users/{id}", deleteUser)
	})

	// Compose wrappers with Chain / ChainFunc.
	app.Get("/admin", sim.ChainFunc(auth)(adminPanel))

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := app.Run(ctx, ":8080"); err != nil {
		log.Fatal(err)
	}
}

func auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func listUsers(w http.ResponseWriter, _ *http.Request) {
	_, _ = fmt.Fprintln(w, "list users")
}

func getUser(w http.ResponseWriter, r *http.Request) {
	_, _ = fmt.Fprintf(w, "user %s", r.PathValue("id"))
}

func createUser(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusCreated)
	_, _ = fmt.Fprintln(w, "user created")
}

func updateUser(w http.ResponseWriter, r *http.Request) {
	_, _ = fmt.Fprintf(w, "user %s updated", r.PathValue("id"))
}

func deleteUser(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

func adminPanel(w http.ResponseWriter, _ *http.Request) {
	_, _ = fmt.Fprintln(w, "admin")
}
```

Save it as `main.go` and run it:

```bash
go run main.go
```

Open http://localhost:8080/ to see "welcome", and http://localhost:8080/api/users
for the user list.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for how to report bugs, suggest
features, improve docs, write tests, and submit changes.

## Acknowledgements

Sim's design was inspired by:

- [chi](https://github.com/go-chi/chi)
- [echo](https://github.com/labstack/echo)
- [gin](https://github.com/gin-gonic/gin)
- [huma](https://github.com/danielgtaylor/huma)
- [grpc-gateway](https://github.com/grpc-ecosystem/grpc-gateway)
- [go-grpc-middleware](https://github.com/grpc-ecosystem/go-grpc-middleware)

## License

MIT, see [LICENSE](LICENSE).
