# TODO

Features that would bring kubemq-ce closer to KubeMQ v3, ranked by value for effort.

## Quick wins (days each)

- [x] **Channel management for SDKs** (1–2 days). Answer the queries the SDKs send to `kubemq.cluster.internal.requests`, so kubemq-js `createChannel`, `listChannels` and `deleteChannel` work. Listing can reuse the snapshot data. Delete is new, and the dashboard could use it too. Done when the three `XFAIL`s in `task smoke` pass. First step: read the request and response format kubemq-js expects.
- [x] **Per-channel permissions** (3–5 days). Every valid token can do everything today. Use the token's `sub` and the unused `authorization` config section for rules like "orders-service may send to `orders.*`". The upstream design was Casbin; simple rules in config would be easier to run. v3: role-based access control.
- [x] **Revocable API keys and an audit log** (1–2 days). Give tokens an ID (`jti`) and keep a revocation list, so a leaked token can be cut off without changing the key. Log who did what at the existing gRPC and REST interceptors. v3: API keys, audit logging.
- [x] **CloudEvents endpoint** (1–2 days). A REST endpoint that accepts CloudEvents and maps them onto events and queues, so anything that can POST JSON can publish. v3: CloudEvents, on by default.
- [x] **MCP server** (2–3 days). Let AI agents send, peek and list channels and read stats. The actions already exist behind `/api/request`; expose them as tools over the official Go MCP SDK, protected by the same tokens. v3: built-in MCP.

## Medium (a week or so)

- [x] **OpenTelemetry tracing**. Add interceptors to gRPC and REST, and carry the trace context in message tags so traces continue from producer through broker to consumer. Metrics are already in Prometheus. v3: OTLP.
- [ ] **KEDA autoscaling**. Autoscale consumers on queue depth. Prometheus already exposes the numbers, so start with docs and an example `ScaledObject`; a proper external scaler can come after. v3: KEDA integration.
- [ ] **MQTT gateway**. Probably a separate MQTT front end (for example mochi-mqtt) mapped onto events and events store. NATS's own MQTT support needs JetStream, which this broker doesn't use; check what the embedded NATS fork in `kubemq-io/broker` supports first. v3: native MQTT.

## The big one

- [ ] **Move storage to JetStream**. This is the path to replicated, clustered storage. NATS Streaming, the current storage layer, is deprecated upstream; JetStream has replication built in (Raft), which is essentially what v3 added. Weeks of work: queue behaviour (visibility timeouts, peek, ack-all, dead-letter queues) is implemented in `pkg/client` on top of NATS Streaming and needs rewriting.

## Not planned

- **Native Kafka, AMQP, SQS or Pub/Sub protocols:** each is a large project. Users who need them are better served by real Kafka or RabbitMQ.
- **More SDKs:** the upstream SDKs already work against this server.
- **FIPS builds:** only matter if certification is needed.

## Suggested order

1. KEDA docs and an example `ScaledObject`: small, and useful to anyone running consumers on Kubernetes.
2. JetStream, as a separate project, if HA is wanted.
3. MQTT, if devices need to publish directly.
