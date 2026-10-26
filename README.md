# kubemq-ce

A fork of [KubeMQ Community](https://github.com/kubemq-io/kubemq-community), the open-source edition of the KubeMQ message broker. It is a single binary that serves queues, pub/sub (events and events store) and RPC (commands and queries) over gRPC and REST, with a built-in web dashboard. Licensed under Apache-2.0, like upstream.

## How this fork differs from upstream

- **No usage reporting.** Upstream sends a usage report (hostname, a host fingerprint, message counts and environment) to `api.kubemq.io` at startup and every 5 minutes. This fork removes that code entirely.
- **Runs on Kubernetes.** Upstream refuses to start when it detects a Kubernetes pod. This fork starts there the same way it does in a standalone Docker container.

The server is single-node: running several replicas gives you separate brokers with separate storage, not a cluster.

The upstream Docker image (`kubemq/kubemq-community`) and upstream release binaries do not include these changes. Build from this repository instead.

## Web interface

Browse to [http://localhost:8080/](http://localhost:8080/) once the server is running to manage the broker from the dashboard.

![](images/dashboard.png "KubeMQ Dashboard")

# Installation

There are no prebuilt images or release binaries for this fork yet.

## Docker

Build the image and run it:

```bash
docker build --build-arg VERSION=v2.4.0 -t kubemq-ce .
docker run -d -p 8080:8080 -p 50000:50000 -p 9090:9090 kubemq-ce
```

Ports: `8080` web interface and API, `50000` gRPC, `9090` REST.

## From source

Requires Go 1.19 or later.

```bash
go build -o kubemq -ldflags "-X 'main.version=v2.4.0'" .
./kubemq server
```

The server writes its effective configuration to `./config.yaml` and stores data in `./store` (relative to the working directory).

## Kubernetes

Run the image built above as a single replica, and mount a persistent volume at `/kubemq/store` so messages survive pod restarts.

# KubeMQ Connectors

|Name | Description | Github | 
|--|--|--| 
| **KubeMQ Targets** | Connect KubeMQ Message Broker with external systems and cloud services |[KubeMQ Targets](https://github.com/kubemq-io/kubemq-targets)|
| **KubeMQ Sources** | Connects external systems and cloud services with KubeMQ message queue broker |[KubeMQ Sources](https://github.com/kubemq-io/kubemq-sources)|
| **KubeMQ Bridges** | bridge, replicate, aggregate, and transform messages between KubeMQ |[KubeMQ Bridges](https://github.com/kubemq-io/kubemq-bridges)|

# KubeMQ CLI

KubeMQ community edition comes with built-in CLI tool for interaction with KubeMQ server. Check out the docs:

* [kubemq commands](docs/cli/kubemq_commands.md)  - Execute
  Kubemq 'commands'
* [kubemq events](docs/cli/kubemq_events.md)     - Execute
  Kubemq 'events' Pub/Sub commands
* [kubemq events_store](docs/cli/kubemq_events_store.md)     -
  Execute Kubemq 'Events-Store' Pub/Sub commands
* [kubemq queries](docs/cli/kubemq_queries.md)   - Execute
  Kubemq 'queries'
* [kubemq queues](docs/cli/kubemq_queues.md)     - Execute
  Kubemq 'queues' commands

# KubeMQ SDKs

KubeMQ SDKs support list:

| SDK | Github   |  
|:----|:---|  
| C#    |  https://github.com/kubemq-io/kubemq-CSharp  |  
| Java    | https://github.com/kubemq-io/kubemq-Java |  
| Python    |  https://github.com/kubemq-io/kubemq-Python  |  
| Node    |  https://github.com/kubemq-io/kubemq-js |  
| Go    | https://github.com/kubemq-io/kubemq-go |  
| REST    |  https://postman.kubemq.io/ |  

## KubeMQ Cookbooks

| SDK | Github   |  
|:----|:---|  
| C#    |  https://github.com/kubemq-io/csharp-sdk-cookbook  |  
| Java    | https://github.com/kubemq-io/java-sdk-cookbook |  
| Python    |  https://github.com/kubemq-io/python-sdk-cookbook |  
| Node    |  https://github.com/kubemq-io/node-sdk-cookbook |  
| Go    | https://github.com/kubemq-io/go-sdk-cookbook|  

## Documentation

Visit our [Extensive KubeMQ Documentation](https://docs.kubemq.io/).

## Support

You can reach us at:

- [**Email**](mailto:support@kubemq.io)
- [**Slack**](https://kubemq.slack.com)
  - [Invitation](https://join.slack.com/t/kubemq/shared_invite/enQtNDk3NjE1Mjg1MDMwLThjMGFmYjU1NTVhZWRjZTRjYTIxM2E5MjA5ZDFkMWUyODI3YTlkOWY2MmYzNGIwZjY3OThlMzYxYjYwMTVmYWM)
- [**Open an issue**](https://github.com/kubemq-io/kubemq-community/issues)
