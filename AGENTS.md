# AGENTS Guidelines for This Repository

`sim` is a minimal HTTP web framework for Go built on `net/http` and
`http.ServeMux`. Module `github.com/qm012/sim`; Go version follows
`go.mod`.

## 1. Verify Before Finishing

* Run `make test` (`go test -v ./...`) and `make lint` before finishing;
  `make lint` checks formatting and the linters in `.golangci.yml`.
* `go mod tidy` and `go fix` must leave no diff — CI enforces both.

## 2. Keep Dependencies at Zero

* The root module stays stdlib-only: `depguard` in `.golangci.yml`
  allows only `$gostd` and `github.com/qm012/sim`.
* Tooling dependencies live in `tools/golangci-lint.mod`, never the
  root `go.mod`.

## 3. Conventions

* Tests: co-located `*_test.go`, table-driven per existing conventions.
  New files start with the full MIT header:
  ```go
  // Copyright (c) 2026 The Sim Authors
  // Use of this source code is governed by a MIT license
  // that can be found in the LICENSE file.
  ```
* One logical change per PR, with tests — see [CONTRIBUTING.md](CONTRIBUTING.md).

## 4. Commands

| Command       | Purpose                                          |
|---------------|--------------------------------------------------|
| `make test`   | `go test -v ./...` — full test suite             |
| `make lint`   | golangci-lint via `tools/golangci-lint.mod`      |
| `go mod tidy` | Sync `go.mod` / `go.sum` with imports            |
