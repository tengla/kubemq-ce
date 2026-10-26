# kubemq-ce

A self-hosted message broker for queues, pub/sub and request/reply, in a single binary with a built-in web dashboard. It speaks gRPC and REST, and runs anywhere from a laptop to a Kubernetes pod.

kubemq-ce is a fork of [KubeMQ Community](https://github.com/kubemq-io/kubemq-community), licensed under Apache-2.0 like upstream.

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="images/dashboard-dark.png">
  <img alt="The kubemq-ce dashboard: health, throughput, backlog and error-rate tiles, node CPU and heap gauges, and a throughput chart by channel type" src="images/dashboard-light.png">
</picture>

## How it differs from upstream

- **No usage reporting.** Upstream sends hostname, a host fingerprint, message counts and environment to `api.kubemq.io` at startup and every 5 minutes. That code is gone.
- **Runs on Kubernetes.** Upstream refuses to start inside a Kubernetes pod. kubemq-ce starts there like it does anywhere else.
- **A new dashboard, built from source.** Upstream ships its dashboard only as a compiled bundle. This one lives in [`web/`](web) (Vue 3 + Vite) and covers every channel type. See [Web dashboard](#web-dashboard).
- **Current dependencies.** Go 1.26 and up-to-date gRPC, Prometheus, echo and NATS libraries.
- **Fixes.** The dashboard no longer shows another server's metrics when two run on the same host, and links to individual channels work on reload.

The server is single-node: several replicas are separate brokers with separate storage, not a cluster.

The upstream Docker image (`kubemq/kubemq-community`) and release binaries don't include any of this. Build from this repository.

## Quick start

There are no prebuilt images or binaries yet.

### Docker

```bash
docker build --build-arg VERSION=v2.4.0 -t kubemq-ce .
docker run -d -p 8080:8080 -p 50000:50000 -p 9090:9090 kubemq-ce
```

Then open [http://localhost:8080](http://localhost:8080).

### From source

Requires Go 1.26 or later. The dashboard is committed in `assets/` and embedded in the binary, so Node.js is only needed if you change it.

```bash
task build          # or: go build -o kubemq -ldflags "-X 'main.version=v2.4.0'" .
./kubemq server
```

### Kubernetes

Run the image as a single replica and mount a persistent volume at `/kubemq/store` so messages survive pod restarts.

### Ports

| Port | Serves | Override |
|---|---|---|
| 8080 | Web dashboard and its API | `API_PORT` |
| 50000 | gRPC | `GRPC_PORT` |
| 9090 | REST and websockets | `REST_PORT` |

## Configuration

The server reads `config.yaml` from the directory of the binary, or from the working directory. You can also point the `CONFIG` environment variable at a file, or pass the whole file base64-encoded. On startup it writes the effective configuration back to that file. Data is stored in `./store` by default.

Any setting can be overridden with an environment variable named after its path: `Section.Field` becomes `SECTION_FIELD`, for example `API_PORT`, `QUEUE_MAX_RECEIVE_COUNT` or `STORE_STORE_PATH`.

## Web dashboard

The dashboard runs on port 8080, next to the broker.

- **Home**: health, throughput, queue backlog, error rate and volume, CPU and heap for the node, a throughput chart by channel type, and the busiest channels.
- **Channel lists**: one page each for queues, events, events store, commands and queries, with search, an active-only filter and per-channel rates. You can create channels and send messages from here.
- **Channel pages**:
  - **Queues**: peek at or pull waiting messages, stream messages one at a time and acknowledge or reject each, and clear the queue.
  - **Events and events store**: subscribe live, optionally in a consumer group. Events store subscriptions can replay from the first or last message, a sequence number, a point in time, or a number of seconds ago.
  - **Commands and queries**: receive requests and answer them by hand, or reply to them automatically.
  - **Every type**: watch a channel's traffic without consuming it, and list its clients.

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="images/queue-dark.png">
  <img alt="A queue's page: message counts, and the Peek tab listing waiting messages with their JSON bodies" src="images/queue-light.png">
</picture>

> [!WARNING]
> The dashboard has no authentication. Anyone who can reach port 8080 can read, send and delete messages. Don't expose it to untrusted networks.

## Clients

The `kubemq` binary is also a command-line client:

```bash
./kubemq queues send jobs.email '{"to": "someone@example.com"}'
./kubemq queues receive jobs.email
./kubemq events receive orders.created
```

See the CLI reference for [queues](docs/cli/kubemq_queues.md), [events](docs/cli/kubemq_events.md), [events store](docs/cli/kubemq_events_store.md), [commands](docs/cli/kubemq_commands.md) and [queries](docs/cli/kubemq_queries.md).

kubemq-ce keeps upstream's gRPC and REST API, so the upstream KubeMQ SDKs connect to it: [Go](https://github.com/kubemq-io/kubemq-go), [Java](https://github.com/kubemq-io/kubemq-Java), [Python](https://github.com/kubemq-io/kubemq-Python), [Node](https://github.com/kubemq-io/kubemq-js) and [C#](https://github.com/kubemq-io/kubemq-CSharp). Upstream's [documentation](https://docs.kubemq.io/) explains the messaging patterns.

## Development

```bash
task build_all                                       # rebuild the dashboard, then the binary
npm --prefix web ci && npm --prefix web run dev      # dashboard dev server with hot reload
go test ./...                                        # tests (the integration tests take several minutes)
```

The dashboard dev server proxies API calls to a broker on `http://localhost:8080`. Set `BROKER_URL` to use another one. Run `task web` and commit `assets/` after changing the dashboard, since `go build` embeds whatever is there.

Report bugs in this repository's [issues](https://github.com/tengla/kubemq-ce/issues).

## License

[Apache License 2.0](LICENSE). KubeMQ is a trademark of its owner; this project is not affiliated with or endorsed by KubeMQ.
