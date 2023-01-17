# MongoDB Receiver

**NOTE**: This is a fork of the OpenTelemetry Collector Contrib MongoDB Receiver at commit [2475cc7460a51924ecb3e80e40edc71fe0eaadc2](https://github.com/open-telemetry/opentelemetry-collector-contrib/tree/2475cc7460a51924ecb3e80e40edc71fe0eaadc2/receiver/mongodbreceiver). It keeps compatibility with versions of MongoDB that are earlier than 4.0.

| Status                   |           |
| ------------------------ |-----------|
| Stability                | [beta]    |
| Supported pipeline types | metrics   |
| Distributions            | [contrib] |

This receiver fetches stats from a MongoDB instance using the [golang
mongo driver](https://github.com/mongodb/mongo-go-driver). Stats are collected
via MongoDB's `dbStats` and `serverStatus` commands.

## Purpose

The purpose of this receiver is to allow users to monitor metrics from standalone MongoDB clusters. This includes non-Atlas managed MongoDB Servers.

## Prerequisites

This receiver supports MongoDB versions:

- 2.6
- 3.0+
- 4.0+
- 5.0

Mongodb recommends to set up a least privilege user (LPU) with a [`clusterMonitor` role](https://www.mongodb.com/docs/v5.0/reference/built-in-roles/#mongodb-authrole-clusterMonitor) in order to collect metrics. Please refer to [lpu.sh](./testdata/integration/scripts/lpu.sh) for an example of how to configure these permissions.


## Configuration

The following settings are optional:

- `hosts` (default: [`localhost:27017`]): list of `host:port` or unix domain socket endpoints.
  - For standalone MongoDB deployments this is the hostname and port of the mongod instance
  - For replica sets specify the hostnames and ports of the mongod instances that are in the replica set configuration. If the `replica_set` field is specified, nodes will be autodiscovered.
  - For a sharded MongoDB deployment, please specify a list of the `mongos` hosts.
- `username`: If authentication is required, the user can with `clusterMonitor` permissions can be provided here.
- `password`: If authentication is required, the password can be provided here.
- `collection_interval`: (default = `1m`): This receiver collects metrics on an interval. This value must be a string readable by Golang's [time.ParseDuration](https://pkg.go.dev/time#ParseDuration). Valid time units are `ns`, `us` (or `µs`), `ms`, `s`, `m`, `h`.
- `replica_set`: If the deployment of MongoDB is a replica set then this allows users to specify the replica set name which allows for autodiscovery of other nodes in the replica set.
- `timeout`: (default = `1m`) The timeout of running commands against mongo.
- `tls`: (defaults defined [here](https://github.com/open-telemetry/opentelemetry-collector/blob/main/config/configtls/README.md)): TLS control. By default insecure settings are rejected and certificate verification is on.

### Example Configuration

```yaml
receivers:
  mongodb:
    hosts:
      - endpoint: localhost:27017
    username: otel
    password: $MONGODB_PASSWORD
    collection_interval: 60s
    tls:
      insecure: true
      insecure_skip_verify: true
```

The full list of settings exposed for this receiver are documented [here](./config.go) with detailed sample configurations [here](./testdata/config.yaml).

## Metrics

The following metric are available with versions:
- `mongodb.extent.count` < 4.4 with mmapv1 storage engine
- `mongodb.session.count` >= 3.0 with wiredTiger storage engine
- `mongodb.cache.operations` >= 3.0 with wiredTiger storage engine
- `mongodb.connection.count` with attribute `active` is available >= 4.0
- `mongodb.index.access.count` >= 4.0

Details about the metrics produced by this receiver can be found in [metadata.yaml](./metadata.yaml)

[beta]:https://github.com/open-telemetry/opentelemetry-collector#beta
[contrib]:https://github.com/open-telemetry/opentelemetry-collector-releases/tree/main/distributions/otelcol-contrib
