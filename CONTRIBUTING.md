# Contributing

Thanks for considering contributing to Sim. This guide covers how to
report issues and submit changes.

## Issues

- Search existing issues before opening a new one.
- Describe the problem or feature clearly, and include a minimal
  reproduction or example if possible.
- For general guidance on writing good issues and pull requests, see
  the [oss-contribution-guide](https://github.com/suzuki-shunsuke/oss-contribution-guide).

## Prerequisites

- Go 1.26+

The root project has no third-party dependencies; only the Go standard
library is allowed in its code.

## Getting Started

1. Fork the repository and clone your fork:

   ```bash
   git clone https://github.com/<your-username>/sim.git
   cd sim
   ```

2. Create a topic branch:

   ```bash
   git checkout -b my-change
   ```

### Keep your fork up to date

If you contribute repeatedly, your fork can drift behind `main`
between contributions — especially after a longer gap — and a pull
request opened from a stale fork may conflict. Sync before starting
each new change.

One-time setup: add the original repository as a remote.

```bash
git remote add upstream https://github.com/qm012/sim.git
```

Then, before each new contribution, update your fork's `main`:

```bash
git fetch upstream
git checkout main
git rebase upstream/main
git push origin main
```

If a topic branch has been open for a while, rebase it onto the latest
`main` and resolve any conflicts:

```bash
git rebase upstream/main
```

## Making Changes

- Keep each pull request focused on one change.
- Add or update tests for your change, following the existing
  `*_test.go` conventions.
- New files must start with the MIT copyright header used in the rest
  of the repository.
- Do not add third-party dependencies to the root project; CI enforces
  this with `depguard`.
- Use English for code comments and documentation.
- Add doc comments for exported functions and types.
- Run the checks below before pushing.

## Running Checks

```bash
make test              # go test -v ./...
make lint              # golangci-lint (checks formatters and linters)
```

`make lint` checks the project formatters (gofmt, gofumpt, goimports)
alongside the linters in `.golangci.yml`.
CI runs tests with `-race` and verifies `go mod tidy` and `go fix`
leave no diff, so keep the module tidy:

```bash
go mod tidy
```

## Pull Request Title and Description

All changes reach `main` through pull requests: never push commits
directly to `main`, and never merge or rebase another branch into
`main` outside a pull request. Pull requests are squash-merged: on
merge, the title becomes the commit subject on `main` and the
description becomes the commit body. The format below therefore
applies to the pull request — individual commits in your branch only
need to be clear enough for review.

This project uses [Conventional Commits](https://www.conventionalcommits.org/):
the title describes the `what` behind the change; the description
explains the `why`.

### Title

```
<type>: <description>
<type>(<scope>): <description>
```

Guidelines:

- Prefer the imperative mood: start with a lowercase verb (for example
  `add`, `fix`) and skip the trailing period.
- Add a `scope` when the change is confined to a single component (for
  example `feat(chain):`, `chore(ci):`); leave it out for changes that
  span the whole project.
- Keep the title short and specific; it is the first thing reviewers
  see.

Common types:

| Type       | Description                                             |
|------------|---------------------------------------------------------|
| `feat`     | A new feature                                           |
| `fix`      | A bug fix                                               |
| `docs`     | Documentation only changes                              |
| `style`    | Changes that do not affect the meaning of the code      |
| `refactor` | Code change that neither fixes a bug nor adds a feature |
| `perf`     | Performance improvements                                |
| `test`     | Adding or correcting tests                              |
| `chore`    | Changes to the build process or auxiliary tools         |
| `ci`       | Changes to CI configuration and scripts                 |

Examples:

```
feat(chain): add ChainFunc and move Chain into chain.go
chore(ci): add test coverage reporting
fix: preserve wrapper order in Chain
docs: document Group path resolution
```

### Description

Use the description when the title alone does not tell the whole
story: motivation, tradeoffs, or context that is not obvious from the
diff. Reference the issue the pull request fixes, if any, with a
closing keyword such as `Fixes #123` so it closes automatically on
merge. Aim to wrap the description at 72 characters.

```
ChainFunc composes the same wrappers as Chain for func-typed
registration methods such as App.Get and App.Put, so middleware
written for Chain works unchanged with the verb helpers. Keep both
in chain.go so the composition helpers live next to each other.

Signed-off-by: FirstName LastName <email@example.com>
```

A `Signed-off-by:` trailer is not required. If you want to add one,
it certifies that you have the right to contribute the change
([Developer Certificate of Origin](https://developercertificate.org/)).

## Submitting a Pull Request

1. Push your branch to your fork and open a pull request against
   `main`.
2. Write the title and description as specified above.
3. Make sure CI passes, and address review feedback on the same branch.
