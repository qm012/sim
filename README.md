# Sim

[![Latest Release](https://img.shields.io/github/v/release/qm012/sim)](https://github.com/qm012/sim/releases)
[![Tests](https://github.com/qm012/sim/actions/workflows/testing.yml/badge.svg)](https://github.com/qm012/sim/actions/workflows/testing.yml)
[![Lint](https://github.com/qm012/sim/actions/workflows/golangci-lint.yml/badge.svg)](https://github.com/qm012/sim/actions/workflows/golangci-lint.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/qm012/sim.svg)](https://pkg.go.dev/github.com/qm012/sim)
[![Go 1.26+](https://img.shields.io/badge/Go-1.26%2B-00ADD8?logo=go)](https://go.dev)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

**Not another web framework — the missing layer on top of `net/http`.**

Sim (short for *simple*) is a minimal HTTP web framework for Go, built on
top of `net/http` and `http.ServeMux` — no third-party dependencies. It
adds wrappers and utilities while keeping stdlib handlers intact and
native performance untouched. Simple, not simplistic.

## Features

**Core**

- Zero dependencies — only the Go standard library
- Method-based routing: `Get`, `Post`, `Put`, `Delete`, `Patch`,
  `Options`, `Head`, `Connect`, `Trace`, and `Any`
- Routing follows the [net/http.ServeMux](https://pkg.go.dev/net/http#ServeMux) patterns
- Route groups under a common prefix
- Standard `net/http` handlers work everywhere — no framework-specific
  context type to learn
- Wrapper composition with `Chain` and `ChainFunc`
- Conditional wrapper application with `Selector`
- Response helpers: `JSON` (with `EscapeForHTML` / `Indented` options),
  `XML`, `Text`, `Bytes`, `Stream` for chunked streaming, and
  `Attachment` for file downloads
- Request binding into structs with `query`, `form`, `path`, `header`
  and `JSON`/`XML` tags
- Graceful shutdown with `Run`

**Built-in wrappers**

| Wrapper              | What it does                                                                            |
|----------------------|-----------------------------------------------------------------------------------------|
| `ClientIPResolution` | Resolves the real client IP behind trusted proxies and adds `client_ip` to request logs |
| `RequestLogging`     | Writes structured `slog` records per request                                            |
| `Recovery`           | Turns panics into a logged stack trace and HTTP 500 instead of a crash                  |

`Default` bundles all three wrappers, ready to use with no configuration.

**Request binding**

- Bind requests into your own structs: `BindJSON`, `BindXML`,
  `BindQuery`, `BindForm`, `BindPath` and `BindHeader`
- Struct tags with `default=` values, embedded structs, multipart file
  uploads and map targets
- Validation via `Validator`, custom formats via `Decoder`
- Read the request body more than once with `BufferBody`

See the [package documentation](https://pkg.go.dev/github.com/qm012/sim)
for the full struct-tag rules.

## Installation

Requires Go 1.26+.

```bash
go get github.com/qm012/sim
```

## Quick start

```go
package main

import (
	"context"
	"net/http"

	"github.com/qm012/sim"
)

func main() {
	app := sim.Default()
	app.Get("/", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("hello, sim"))
	})
	_ = app.Run(context.Background(), ":8080")
}
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
	// NewApp starts with no wrappers; register them explicitly,
	// outermost first.
	app := sim.NewApp()

	logging := new(sim.RequestLogging)
	app.Use(
		new(sim.ClientIPResolution).Handler,
		// Log every request except the ping endpoint.
		sim.Selector(logging.Handler, func(r *http.Request) bool {
			return r.URL.Path != "/ping"
		}),
		new(sim.Recovery).Handler,
	)

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

func listUsers(w http.ResponseWriter, r *http.Request) {
	// BindQuery fills a struct from the URL query; page defaults to 1.
	q, err := sim.BindQuery[struct {
		Page int `query:"page,default=1"`
	}](r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	_, _ = fmt.Fprintf(w, "list users, page %d\n", q.Page)
}

func getUser(w http.ResponseWriter, r *http.Request) {
	_, _ = fmt.Fprintf(w, "user %s", r.PathValue("id"))
}

// user is the payload createUser decodes from the request body.
type user struct {
	Name string `json:"name"`
	Age  int    `json:"age"`
}

func createUser(w http.ResponseWriter, r *http.Request) {
	u, err := sim.BindJSON[user](r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusCreated)
	_, _ = fmt.Fprintf(w, "user %s created\n", u.Name)
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

Open http://localhost:8080/ to see "welcome", and
http://localhost:8080/api/users for the user list. The endpoints speak
HTTP, so try binding too:

```bash
curl 'localhost:8080/api/users?page=2'
curl -X POST localhost:8080/api/users -d '{"name":"alice","age":30}'
```

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
