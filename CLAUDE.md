# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

KubeMQ Community: the open-source, single-binary KubeMQ message broker (Go module `github.com/kubemq-io/kubemq-community`). The same `kubemq` binary is both the server (`kubemq server`) and a CLI client (`kubemq events|events_store|commands|queries|queue ...`).

## Commands

```bash
go build -o kubemq -ldflags "-X 'main.version=v2.4.0'" .   # build (version is injected via ldflags)
go run . server                                           # run broker: gRPC :50000, REST :9090, web UI/API :8080
go test ./...                                             # all tests
go test ./interfaces/grpc -run TestName -v                # single test
task lint                                                 # golangci-lint run --disable gocritic --enable misspell --disable gosec
task docker                                               # build image kubemq/kubemq-community:<VERSION>
```

`Taskfile.yml` (go-task) holds the release flow (`task release` = git tag + multi-arch buildx push + goreleaser). `VERSION` there is the release version; bump it for releases. The default `task` target builds `kubemq.exe` (Windows-oriented).

## Architecture

Startup path: `main.go` → `cmd/root` (cobra; loads config, sets up logging, registers subcommands) → `cmd/server` → `services.Start` then `interfaces.StartInterfaces`. Shutdown runs in reverse on SIGINT/SIGTERM.

Layers, bottom to top:

- **`services/broker`**: embeds a NATS server plus NATS Streaming (stan) from the `kubemq-io/broker` fork. They talk over an in-memory pipe (`config.Broker.MemoryPipe`), not TCP. Stan file storage lives in `Store.StorePath/<Host>`. `services.Start` blocks until the broker reports ready.
- **`pkg/client`**: internal NATS/stan clients that map KubeMQ patterns onto NATS subjects with prefixes (`_EVENTS_.`, `_EVENTS_STORE_.`, `_COMMANDS_.`, `_QUERIES_.`, `_QUEUES_.`). Queue semantics (visibility, ack, peek, streaming downstream/upstream) are implemented here on top of stan. Includes `QueuePool` for pooled queue clients.
- **`services/array`**: the façade every transport calls (`SendEvents`, `SubscribeToCommands`, queue upstream/downstream, ...). It owns shared clients and client pools, tracks per-subscription clients, and wraps calls with `pkg/monitor` middleware for metrics.
- **`interfaces/grpc`** and **`interfaces/rest`**: public transports. gRPC implements the `kubemq-io/protobuf` service. REST uses echo plus websockets for subscriptions. Both delegate straight to `services.Array`.
- **`services/api`** + **`pkg/api`**: the web dashboard backend (echo, port `Api.Port`). It serves the prebuilt Nuxt UI in `assets/` (embedded via `//go:embed` in `main.go` and treated as a build artifact, not source) and exposes stats/snapshots plus actions (send/receive/purge) that go through an internal client. `services/metrics` collects Prometheus counters for it.
- **`services/report`**: periodic usage reporting.

The `cmd/<pattern>` packages are CLI clients that connect to a running server using the `client` config section (gRPC host/port, API address).

## Configuration

`config/` holds one file per section (`grpc.go`, `queue.go`, `store.go`, ...), each with a `default*Config()` and a `Validate()`. Each section is aggregated in `config.Config`.

- Sources, in order: the `CONFIG` env var (base64-encoded YAML/TOML, or a file path), then `./config.yaml` (looked up next to the executable first, then in CWD), then defaults. Viper unmarshals into the struct.
- Env overrides are registered per field with `bindViperEnv("Section.Field")`, which maps to `SECTION_FIELD` in upper snake case (e.g. `Broker.MaxPayload` → `BROKER_MAX_PAYLOAD`). When you add a config field, add it to that section's `bindViperEnv` list and its defaults.
- **Side effect:** each config load writes the merged config back to the loaded file, or to `./config.yaml` in CWD if none was loaded. This is why `config.yaml` files appear in package directories after tests run.
- `config.GetAppConfig` is a process-wide singleton (`sync.Once`), and some code (e.g. `pkg/monitor`) reads it globally. Tests use `config.GetCopyAppConfig` for isolated copies.
- Authentication, authorization (casbin) and routing exist as config sections, but in this edition they are only referenced from `config/`.

## Tests

Integration-style tests (`interfaces/grpc`, `interfaces/rest`, `services/array`, `pkg/client`) start a full in-process broker per test. Each test gets a fresh `pipe.NewPipe`, unique ports from an atomic counter starting at 55555, and a temp store dir created inside the package directory (e.g. `test_grpc*`). They are slow and use `leaktest` for goroutine leak checks. The per-package `config.yaml` files are committed and get rewritten by test runs, so don't commit incidental diffs to them. Delete stray temp store dirs.
