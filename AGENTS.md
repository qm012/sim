# AGENTS Guidelines for This Repository

`sim` is a minimal HTTP web framework for Go built on `net/http` and
`http.ServeMux`. Module `github.com/qm012/sim`; Go version follows
`go.mod`.

## 1. Keep Dependencies at Zero

* The root module stays stdlib-only: `depguard` in `.golangci.yml`
  allows only `$gostd` and `github.com/qm012/sim`.
* Tooling dependencies live in `tools/golangci-lint.mod`, never the
  root `go.mod`.

## 2. Development Practices

* Run `make test` (`go test -v ./...`) and `make lint` before finishing;
  `make lint` checks formatting and the linters in `.golangci.yml`.
* `go mod tidy` and `go fix` must leave no diff — CI enforces both.
* Tests: co-located `*_test.go`, table-driven per existing conventions.
  New files start with the full MIT header:
  ```go
  // Copyright (c) 2026 The Sim Authors
  // Use of this source code is governed by a MIT license
  // that can be found in the LICENSE file.
  ```
* One logical change per PR, with tests — see [CONTRIBUTING.md](CONTRIBUTING.md).

## 3. Go Documentation: Local GOROOT First

* **Lookup order is mandatory: local toolchain first, web only as an
  exception.** For Go standard library, language, or project-symbol
  questions:
  1. `go doc <pkg>` / `go doc <pkg>.<Symbol>` for API signatures and
     doc comments (e.g. `go doc net/http.ServeMux`). Works for this
     module too (e.g. `go doc github.com/qm012/sim.Chain`).
  2. `go doc -all <pkg>` for a package's full documentation, and
     `go doc -src <pkg>.<Symbol>` to jump straight to the source.
  3. Read files directly under the GOROOT (`go env GOROOT`,
     typically `<goroot>/src/<pkg>`) when even more context is
     needed.
* Do **not** use web search for Go documentation unless the user
  explicitly asks to search the web, or the information genuinely
  cannot be found in the local GOROOT (e.g. a proposal or release
  note not shipped with the toolchain).

## 4. Commands

| Command       | Purpose                                          |
|---------------|--------------------------------------------------|
| `make test`   | `go test -v ./...` — full test suite             |
| `make lint`   | golangci-lint via `tools/golangci-lint.mod`      |
| `go mod tidy` | Sync `go.mod` / `go.sum` with imports            |
| `go doc <pkg>`| Local Go docs — always prefer over web search    |
