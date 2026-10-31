# TODO

The [roadmap](#roadmap) at the end is the plan for making kubemq-ce a production-grade broker. The sections before it list features that would bring it closer to KubeMQ v3, ranked by value for effort.

## Quick wins (days each)

- [x] **Channel management for SDKs** (1–2 days). Answer the queries the SDKs send to `kubemq.cluster.internal.requests`, so kubemq-js `createChannel`, `listChannels` and `deleteChannel` work. Listing can reuse the snapshot data. Delete is new, and the dashboard could use it too. Done when the three `XFAIL`s in `task smoke` pass. First step: read the request and response format kubemq-js expects.
- [x] **Per-channel permissions** (3–5 days). Every valid token can do everything today. Use the token's `sub` and the unused `authorization` config section for rules like "orders-service may send to `orders.*`". The upstream design was Casbin; simple rules in config would be easier to run. v3: role-based access control.
- [x] **Revocable API keys and an audit log** (1–2 days). Give tokens an ID (`jti`) and keep a revocation list, so a leaked token can be cut off without changing the key. Log who did what at the existing gRPC and REST interceptors. v3: API keys, audit logging.
- [x] **CloudEvents endpoint** (1–2 days). A REST endpoint that accepts CloudEvents and maps them onto events and queues, so anything that can POST JSON can publish. v3: CloudEvents, on by default.
- [x] **MCP server** (2–3 days). Let AI agents send, peek and list channels and read stats. The actions already exist behind `/api/request`; expose them as tools over the official Go MCP SDK, protected by the same tokens. v3: built-in MCP.

## Medium (a week or so)

- [x] **OpenTelemetry tracing**. Add interceptors to gRPC and REST, and carry the trace context in message tags so traces continue from producer through broker to consumer. Metrics are already in Prometheus. v3: OTLP.
- [ ] **KEDA autoscaling**. Autoscale consumers on queue depth. Prometheus already exposes the numbers, so start with docs and an example `ScaledObject`; a proper external scaler can come after. v3: KEDA integration.

## The big one

- [x] **Replace the `kubemq-io/broker` fork with `nats-server` and `nats.go`, storage on JetStream**. The fork bundled NATS 2.1.9 (about 2020) and NATS Streaming, which is deprecated with no maintained version, so security fixes no longer came from upstream. This is a maintenance job first; replication and clustering stay optional. See roadmap step 4.

## Not planned

- **Native Kafka, AMQP, SQS or Pub/Sub protocols:** each is a large project. Users who need them are better served by real Kafka or RabbitMQ.
- **More SDKs:** the upstream SDKs already work against this server.
- **FIPS builds:** only matter if certification is needed.
- **MQTT:** it's a protocol for devices on unreliable networks, and services talking to each other are better served by the SDKs over gRPC, which have queues and request/reply that MQTT lacks. Reconsider only if something that speaks nothing but MQTT has to publish, such as an industrial gateway; after roadmap step 4, NATS's own MQTT support makes that a small job. v3: native MQTT.

## Roadmap

What it takes to go from a well-run fork to a broker people can run in production, in order. Each step makes the next one safer.

### 0. Positioning

kubemq-ce is the KubeMQ-compatible broker without the enterprise licence: the same SDKs, API and messaging patterns, fully open source under Apache-2.0, in one binary with no telemetry. KubeMQ's enterprise edition is priced for large companies, which leaves out the teams that would otherwise use it.

Who it's for, in order:

1. **Teams already on KubeMQ.** Their producers and consumers are written against the KubeMQ SDKs. Moving to RabbitMQ or Kafka means rewriting them; moving to kubemq-ce means changing an address. This is the strongest case, so compatibility with the upstream SDKs is the product.
2. **New projects that want a simple broker.** Five clear patterns, request/reply built in, a dashboard, and one binary to run. Be honest in the README that NATS, RabbitMQ and Redpanda are free and mature alternatives too; kubemq-ce competes on simplicity there, not on price.

The enterprise features such teams would pay for are mostly done: access control, audit logging, revocable API keys, OpenTelemetry, CloudEvents and MCP. The big remaining gap is clustering and high availability, which the move to JetStream makes possible (step 4).

- [ ] **Put this at the top of the README.** Lead with compatibility and the open licence, and keep the comparison with upstream factual.
- [ ] **Settle the name before publishing images or announcing anything.** "KubeMQ" is likely a trademark of KubeMQ Ltd, and the Apache-2.0 licence covers the code, not the name. A distinct name with "compatible with KubeMQ" in the description is the safer path for a project others adopt. Check whether "kubemq-ce" is acceptable, or pick a new name and keep the module path change in mind.
- [ ] **Treat SDK compatibility as the main promise.** The SDK smoke tests in step 1 should cover the Go, Python, Java and .NET SDKs, not just kubemq-js, and a compatibility table in the README should say which SDK versions are tested.

### 1. CI and a reliable test suite

- [ ] **Isolate the integration tests.** Every test in a package uses the same fixed API port from its `config.yaml`, and the port counters start at the same number each run, so two runs at once collide. Give each test free ports and its own temp store, as `services/api/mcp_test.go` does, and stop writing `config.yaml` during tests.
- [ ] **Make the suite faster.** It takes about 13 minutes, mostly in `pkg/client` (6 min) and `interfaces/grpc` (4 min). Look for fixed sleeps and per-test broker startups that can be shared.
- [ ] **Run CI on every pull request:** `go vet`, golangci-lint, the Go tests, the dashboard build (check that `assets/` matches `web/`) and `task smoke` against a freshly built server.
- [ ] **Check more SDKs.** Add smoke tests for kubemq-go, kubemq-Python, kubemq-Java and kubemq-CSharp next to the kubemq-js one, and run them all in CI (see step 0).

### 2. Releases

- [ ] **Publish images and binaries.** Multi-arch images and release binaries from CI on each tag (`task release` exists but builds upstream's image name). Sign them, and generate an SBOM.
- [ ] **Version and document changes.** Semantic versions, a changelog, and upgrade notes for anything that changes config or stored data.

### 3. Backups

- [ ] **Backup and restore** of the store, documented and tested, including a restore onto a new node. With one pod and one volume, this is what protects queued and stored messages from a lost disk, both before and after step 4.

### 4. Replace the broker fork with maintained NATS

The `kubemq-io/broker` fork held the NATS server (`server/gnatsd`, version 2.1.9 from about 2020), the NATS Streaming server (`server/stan`) and both clients. NATS Streaming is deprecated with no maintained version, and the fork doesn't look actively maintained, so security fixes in that code would have had to be made here. It's replaced with the NATS project's own libraries: `github.com/nats-io/nats-server/v2`, embedded so kubemq stays one binary with an in-process connection, and `github.com/nats-io/nats.go`, including its `jetstream` package.

This is a maintenance job first. One node stays the default and nothing has to be clustered.

- [x] **Spike first.** Embed current `nats-server` with JetStream and try out what the queue rewrite depends on.
- [x] **Move events, commands and queries to `nats.go`.** They use plain publish/subscribe and request/reply.
- [x] **Rewrite queues and events store on JetStream.** `pkg/jstore`, with queue semantics staying in `pkg/client`.
- [x] **Keep one node the default.** Streams have one replica and file storage under `store/`, so `kubemq server` on a laptop or in a single pod works as it did. `store.maxStorage` and `store.maxMemory` set JetStream's limits.
- [x] **Migrate existing data.** Not migrated: an upgrade starts empty, as the README says, and the server warns when it finds the old files. A migrator would have to keep the fork as a dependency.
- [x] **Remove the fork** from `go.mod`.
- [ ] **Larger stored messages.** JetStream's file store refuses a message over about 32 MiB, while NATS Streaming stored up to `broker.maxPayload` (100 MB by default). Queues and events store now return a clear error above that. Splitting large messages into chunks would lift the limit; do it if someone needs it.
- [ ] **Faster batches.** A stream flushes each message by default, which caps one channel at about 185,000 msg/s (on the machine measured). The old store reached about 477,000 msg/s with no confirmed messages lost on `kill -9`. `PersistMode: AsyncPersistMode` doubles batch speed but lost up to 154 confirmed messages on `kill -9`, and atomic batches are far slower. A real fix means batched flushing in `nats-server` itself.
- [ ] **Optional: clustering.** Three-node clusters with streams raised to three replicas (Raft needs a quorum; two nodes give no fault tolerance), stable node names and a `cluster` config section. A StatefulSet example for Kubernetes. Only needed when the broker must survive a node failure.
- [ ] **Use what JetStream brings.** Deduplication by message ID within a window, so a retried send can't create a duplicate. The key-value store for state such as the query cache. Message scheduling (`Nats-Schedule`) in place of the delay processor. Queues already use work-queue retention.

### 5. Benchmarks

- [ ] **Measure before and after the move.** Throughput and latency per pattern, with message sizes from 100 B to 1 MB, on one node and later on a cluster. Keep the harness in the repo so regressions show up, and publish the numbers.

### 6. Operations

- [ ] **Helm chart** for a single node first, then a cluster, with the persistent volume, probes (`/health`, `/ready`), resource limits and the auth, tracing and audit settings.
- [ ] **Behave well when the disk fills:** refuse new messages with a clear error instead of failing in the storage layer, and expose disk use as a metric. Neither a full disk nor reaching `store.maxStorage` has been tested since the move to JetStream, so first check what a sender sees and whether the server recovers once there's room again.
- [x] **Refuse a store another server is using.** A second server on the same store and host exits with a clear error. The lock belongs to the open file, so a crash doesn't leave it held.
- [x] **Use absolute store paths as given.** `store.storePath: /data` used to become `./data` without a word, so a volume mounted at `/data` was never written to.
- [ ] **KEDA** docs and an example `ScaledObject` (see above).

### 7. Hardening

- [ ] **TLS everywhere by default in the examples**, including the dashboard port, and document running behind a TLS proxy.
- [ ] **Fuzz the inputs that parse untrusted data:** the REST handlers, CloudEvents, the MCP tools and the authorization rules.
- [ ] **Rate limits and payload limits per client,** so one misbehaving client can't starve the others.

### 8. Messaging features

- [ ] **Tag filters on subscriptions,** for example only events with `region=eu`. Messages already carry tags.
- [ ] **Ordering keys for queues:** messages with the same key go to one consumer at a time. Easier on JetStream.

### Thomas name suggestion
- Jolle 


### Claude name suggestions

Candidates, from one web search each at most. None has been checked on GitHub, Docker Hub, domains or trademark registers yet.

- **Sampan:** a small, light, flat-bottomed boat that carries people and goods around harbours. The search found no software by that name.
- **Tenma:** from *tenmasen* (伝馬船), a small boat ferrying between ship and shore. *Tenma* also names Japan's old post-horse relay system for messages. Not searched yet, and it's a common Japanese name, so search results will be crowded.
- **Kobune:** Japanese for "small boat" (小舟). Not searched yet.
- **Aviso:** a small, fast naval ship that carried dispatches. No software turned up in the search, but ECMWF's Aviso notification service, recalled from memory, needs checking.
- **Packetboat:** packet boats carried mail on regular routes, and "packet" is also a networking term. Not searched yet.
- **Lighter:** the barge that carries cargo between ship and shore, with a hint of "lightweight". Not searched yet, and a common word, so hard to search for.
- **Wherry:** a light river boat for passengers and goods. No software turned up, but it's obscure and sounds like "worry".

Ruled out:

- **Skiff:** Notion bought Skiff, the private email and docs service, in 2024.
- **Ketch:** Shipa's open-source Kubernetes deployment engine.
- **Ferry:** taken twice in Kubernetes, by the ferryproxy project and a KubeCarrier component.
- **Kedge:** two Kubernetes projects, a deployment tool and an edge proxy, both apparently dormant.
- **Junk, Junker:** "junk mail" for a messaging product, and Junkers is the German maker of WWII aircraft.
