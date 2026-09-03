# pprof labeling

Labels every request's goroutine with its route pattern via
`sim.PprofLabeling`, so CPU profiles and panic tracebacks can be
attributed to routes. The app serves traffic on `:8080` and the
standard `net/http/pprof` handlers on a separate metrics server at
`:8081`. Every log record also carries a per-request `trace_id`
injected into the context by a tiny middleware and read back by a
custom `slog.Handler`.

## Run

```sh
go run .
```

Then try the two routes:

```sh
curl http://localhost:8080/hello
curl http://localhost:8080/panic
```

`/panic` is recovered by `sim.Recovery` and logged as an ERROR record
with the stack trace and the request details.

## Route labels in panic tracebacks

`PprofLabeling` wraps each request in `runtime/pprof.Do` with a
`pattern=<matched route>` label. The runtime can print those labels in
goroutine tracebacks, controlled by the `tracebacklabels` GODEBUG
setting:

- This module targets Go 1.26, where the setting defaults to `0`.
  Start the example with it enabled to see labels in the panic log:

  ```sh
  GODEBUG=tracebacklabels=1 go run .
  ```

  (PowerShell: `$env:GODEBUG='tracebacklabels=1'; go run .`)
- From Go 1.27 on, the default is `1`. Set `GODEBUG=tracebacklabels=0`
  to switch it back off permanently for the binary.

With labels enabled, `curl http://localhost:8080/panic` produces a
`[Recovery] panic recovered` record whose stack starts with:

```text
goroutine N [running] {pattern: "GET /panic"}:
```

The same labels also appear in `?debug=2` goroutine dumps (see
below) and in CPU profile samples. Even without the setting,
Recovery's structured `request.pattern` attribute identifies the
route.

## CPU profiling with `go tool pprof`

Point `go tool pprof` at the *running* service's metrics port. While
profiling, keep some traffic flowing (`/hello` in a loop):

```sh
go tool pprof -http=:8082 "http://localhost:8081/debug/pprof/profile?seconds=10"
```

The browser UI opens automatically; choose **VIEW → Flame Graph** for
the flame graph (wider = more CPU, the x-axis is not time). Samples
taken on the request goroutine carry the `pattern` label, so you can
profile one route at a time with `-tagfocus`:

```sh
go tool pprof -http=:8082 -tagfocus="pattern:GET /hello" "http://localhost:8081/debug/pprof/profile?seconds=10"
```

`-tagfocus` is a filter: it *drops* every sample without a matching
label. Keep traffic flowing while sampling, and confirm the filter
applied via the `Active filters:` line in the output (or `-raw`, which
shows `pattern:[GET /hello]` on matching samples). Samples from
connection-level goroutines (accept, background reads) carry no
label, so even under load a filtered profile is much smaller than the
full one.

Other profiles work the same way:

```sh
go tool pprof http://localhost:8081/debug/pprof/goroutine
```

The `goroutine` and `goroutineleak` profiles also carry the labels,
but only for goroutines sampled *inside* the labeled section — the
handler must be doing real work when the snapshot is taken, which is
rare for a handler as fast as `/hello`.

`heap`, `allocs`, `block`, `mutex` and `threadcreate` do not carry
labels: their records are per-event, not per-goroutine.

## Execution tracing with `go tool trace`

```sh
curl -o trace.out "http://localhost:8081/debug/pprof/trace?seconds=5"
go tool trace trace.out
```

This complements pprof: instead of statistical samples you get a
timeline of scheduling, GC and blocking events. Note that execution
traces do not carry the pprof goroutine labels — labels only appear
in profiles, tracebacks and `?debug=2` goroutine dumps, so use pprof
for route attribution.

## Goroutine dumps and leaks

`?debug=2` dumps every goroutine as a traceback. With
`GODEBUG=tracebacklabels=1`, goroutines inside the labeled section
start with `{pattern: "GET /hello"}` in their header:

```sh
curl "http://localhost:8081/debug/pprof/goroutine?debug=2"
```

`goroutineleak` reports goroutines that blocked for a long time —
as text or as a pprof profile whose samples carry the labels, so a
leaked request goroutine names its route:

```sh
curl "http://localhost:8081/debug/pprof/goroutineleak?debug=2"
go tool pprof -http=:8082 "http://localhost:8081/debug/pprof/goroutineleak"
```

## Available endpoints

`main.go` registers `pprof.Index` plus `cmdline`, `profile`, `symbol`
and `trace` on `:8081`. Everything else — `goroutine`,
`threadcreate`, `heap`, `allocs`, `block`, `mutex` and Go 1.27's
`goroutineleak` — is served through `pprof.Index` without extra
registration:

```sh
curl "http://localhost:8081/debug/pprof/goroutineleak?debug=1"
```

Browse `http://localhost:8081/debug/pprof/` for the full list.
