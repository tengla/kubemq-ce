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
task web                                                  # build the dashboard (web/) into assets/
npm --prefix web run dev                                  # dashboard dev server; proxies /api (incl. websockets) to BROKER_URL, default http://localhost:8080
task smoke -- localhost:50000                             # kubemq-js SDK smoke test (test/smoke-js) against a running server; KUBEMQ_TOKEN if auth is on
task bench -- -addr localhost:50000 -out new.json         # benchmarks (test/bench) against a running server; `go run ./test/bench compare old.json new.json` compares two runs
```

`Taskfile.yml` (go-task) holds the release flow (`task release` = git tag + multi-arch buildx push + goreleaser). `VERSION` there is the release version; bump it for releases. The default `task` target builds `kubemq.exe` (Windows-oriented).

## Architecture

Startup path: `main.go` → `cmd/root` (cobra; loads config, sets up logging, registers subcommands) → `cmd/server` → `services.Start` then `interfaces.StartInterfaces`. Shutdown runs in reverse on SIGINT/SIGTERM.

Layers, bottom to top:

- **`services/broker`**: embeds `nats-server` with JetStream and no listening port. Clients connect in-process through `pkg/pipe` (`config.Broker.MemoryPipe`), which the broker binds on start. JetStream files live in `Store.StorePath/<Host>/jetstream`. `pkg/storelock` holds `Store.StorePath/<Host>.lock` while the server runs, so a second server on that store exits instead of writing to it. Channel stats come from the server's `Jsz` in-process. `services.Start` blocks until the broker reports ready.
- **`pkg/jstore`**: stored channels on JetStream. One stream per channel (hashed name, the subject is in its config), so sequences count per channel. Subscribers pull through a durable consumer with their own request handling, not `nats.go`'s `Fetch`: a closing subscriber must cancel its waiting request and hand back held messages in order with a NAK, and held messages are renewed with `InProgress`, so `ackWait` only matters after a crash. It also enforces unique client IDs, as NATS Streaming did.
- **`pkg/client`**: internal clients that map KubeMQ patterns onto NATS subjects with prefixes (`_EVENTS_.`, `_EVENTS_STORE_.`, `_COMMANDS_.`, `_QUERIES_.`, `_QUEUES_.`). Events, commands and queries use core NATS. Queue semantics (visibility, ack, peek, streaming downstream/upstream) are implemented here on top of `pkg/jstore`. Includes `QueuePool` for pooled queue clients.
- **`services/array`**: the façade every transport calls (`SendEvents`, `SubscribeToCommands`, queue upstream/downstream, ...). It owns shared clients and client pools, tracks per-subscription clients, and wraps calls with `pkg/monitor` middleware for metrics. `SendQuery` doesn't route queries to `kubemq.cluster.internal.requests` (the SDKs' create/list/delete-channel calls); it hands them to the handler `services/api` registers (`services/api/channels.go`). Queries with a `CacheKey` are answered from `services/array/query_cache.go` when a response is stored.
- **`interfaces/grpc`** and **`interfaces/rest`**: public transports. gRPC implements the `kubemq-io/protobuf` service. REST uses echo plus websockets for subscriptions, and also takes CloudEvents at `/cloudevents/<pattern>[/<channel>]` (`interfaces/rest/cloudevents.go`). Both delegate straight to `services.Array`.
- **`services/api`** + **`pkg/api`**: the web dashboard backend (echo, port `Api.Port`), which also serves the MCP server at `/mcp` (`services/api/mcp.go`, official Go MCP SDK, stateless streamable HTTP). The dashboard part serves `assets/` as static files (embedded via `//go:embed` in `main.go`) and exposes `/api/snapshot` plus actions (send/receive/purge) that go through an internal client. The snapshot is rebuilt every 5 s (`saveInterval`) from `services/metrics`, which gathers the process's Prometheus registry in-process, merged with entity history saved in a local bolt DB under the store path.
- **Dashboard UI**: `web/` (Vue 3 + Vite + vue-router) builds into `assets/` (`index.html` plus hashed files in `assets/app/`); the build empties `assets/` first. Commit the build output: `go build` embeds whatever is in `assets/`. `spaHandler` in `services/api/server.go` serves those files and returns `index.html` for every other path, so routes like `/queues/<channel>` work on reload; channel names often contain dots, so don't key the fallback on file extensions. Pages: Home (`/dashboard`), a list page per channel type (`/queues`, `/events`, `/events-store`, `/commands`, `/queries`) and a channel page (`/<type>/<channel>`) whose tabs use `POST /api/request` and the websocket endpoints (`/api/subscribe/pubsub`, `/api/subscribe/cqrs`, `/api/stream_queue_messages`, and `/api/monitor`, which only forwards traffic after the client sends `start`). All pages share one snapshot poller (`web/src/snapshot.js`).

The `cmd/<pattern>` packages are CLI clients that connect to a running server using the `client` config section (gRPC host/port, API address).

## Configuration

`config/` holds one file per section (`grpc.go`, `queue.go`, `store.go`, ...), each with a `default*Config()` and a `Validate()`. Each section is aggregated in `config.Config`.

- Sources, in order: the `CONFIG` env var (base64-encoded YAML/TOML, or a file path), then `./config.yaml` (looked up next to the executable first, then in CWD), then defaults. Viper unmarshals into the struct.
- Env overrides are registered per field with `bindViperEnv("Section.Field")`, which maps to `SECTION_FIELD` in upper snake case (e.g. `Broker.MaxPayload` → `BROKER_MAX_PAYLOAD`). When you add a config field, add it to that section's `bindViperEnv` list and its defaults.
- **Side effect:** each config load writes the merged config back to the loaded file, or to `./config.yaml` in CWD if none was loaded. This is why `config.yaml` files appear in package directories after tests run. CLI subcommands do this too, and the write isn't atomic: several `kubemq` CLI processes started at once in the same directory can read a truncated file and reset it to defaults (pointing at `localhost:50000`). Scripts that generate traffic should use the REST API or start CLI clients one at a time.
- Tracing (`tracing` section, `pkg/tracing`): `cmd/server` sets up the OTLP exporter and global tracer provider before the services start. gRPC gets the `otelgrpc` stats handler and REST its own echo middleware, added only when tracing is on. Message spans come from `tracing.Send` in the `services/array` send paths and `tracing.Deliver` where messages reach consumers (the subscribe callbacks in `pkg/client`, queue pulls and downstream gets in `services/array`, `createResponseWithMessage` for queue streams); both carry the context in the message's `traceparent` tag and do nothing while tracing is off. A new send or delivery path needs them too. Tests turn tracing on with `tracing.Use` and an in-memory span recorder before starting the server.
- Files the server rereads while running (revoked tokens, authorization rules) go through `pkg/filewatch`, which polls from the request path rather than running a goroutine (tests use `leaktest`).
- `config.GetAppConfig` is a process-wide singleton (`sync.Once`), and some code (e.g. `pkg/monitor`) reads it globally. Tests use `config.GetCopyAppConfig` for isolated copies.
- Authentication is JWT-only: `pkg/authentication` verifies tokens and signs them for `kubemq token`, giving each an ID (`jti`) that `authentication.revokedTokensFile` can revoke. `pkg/guard` combines it with `pkg/authorization` (per-channel rules from the `authorization` section, checked by mapping each protobuf message to send/receive/manage checks in `authorization.ChecksFor`) and `pkg/audit` (the `audit` section). The guard is enforced by interceptors in `interfaces/grpc/middleware/auth.go`, which also check every message received on a stream, and in REST by echo middleware in `interfaces/rest/auth.go` plus an `s.authorize` call in each handler after binding. `Ping` and `/ping` stay open. When adding an RPC or REST route, add its message to `ChecksFor` and authorize it. On `Api.Port`, `services/api/auth.go` guards `/api/*`, `/metrics`, `/config` and `/v1/stats` (Bearer header, or the HttpOnly `kubemq_session_<port>` cookie set by `/api/auth/login`, with an Origin check for cookie requests); the SPA files, `/health`, `/ready` and `/api/auth/*` stay open, and `web/src/session.js` shows the login page on 401. With authorization on, the dashboard needs a rule granting `admin`; MCP tools need the same permissions a client would. CLI commands that call the API server send `client.authToken` via `utils.APIRequest`/`APIHeader`. `/config` serves `Config.Redacted()`, so add any new secret field there.
- Routing exists as a config section, but in this edition it is only referenced from `config/`.

## Tests

Integration-style tests (`interfaces/grpc`, `interfaces/rest`, `services/array`, `pkg/client`) start a full in-process broker per test. Each test gets a fresh `pipe.NewPipe`, unique ports from an atomic counter starting at 55555, and a temp store dir created inside the package directory (e.g. `test_grpc*`). They are slow and use `leaktest` for goroutine leak checks. `services/api/mcp_test.go` (package `api_test`, to start full services without an import cycle) builds its config with `config.GetDefaultConfig` and free ports instead, so it writes no `config.yaml`. The per-package `config.yaml` files are committed and get rewritten by test runs, so don't commit incidental diffs to them. Delete stray temp store dirs.
