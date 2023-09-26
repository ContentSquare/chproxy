---
title: Spread INSERTs among cluster shards
sidebar:
  label: Spreading INSERTs
  order: 1
---

Usually `INSERT`s are sent from app servers located in a limited number of subnetworks. `INSERT`s from other subnetworks must be denied.

All the `INSERT`s may be routed to a [distributed table](https://clickhouse.com/docs/en/engines/table-engines/special/distributed/) on a single node. But this increases resource usage (CPU and network) on the node comparing to other nodes, since it must parse each row to be inserted and route it to the corresponding node (shard).

It would be better to spread `INSERT`s among available shards and to route them directly to per-shard tables instead of distributed tables. The routing logic may be embedded either directly into applications generating `INSERT`s or may be moved to a proxy. Proxy approach is better since it allows re-configuring `ClickHouse` cluster without modification of application configs and without application downtime. Multiple identical proxies may be started on distinct servers for scalability and availability purposes.

The following minimal `chproxy` config may be used for [this use case](https://github.com/ContentSquare/chproxy/blob/master/config/examples/spread.inserts.yml):
```yml
server:
  http:
      listen_addr: ":9090"

      # Networks with application servers.
      allowed_networks: ["10.10.1.0/24"]

users:
  - name: "insert"
    to_cluster: "stats-raw"
    to_user: "default"

clusters:
  - name: "stats-raw"

    # Requests are spread in `round-robin` + `least-loaded` fashion among nodes.
    # Unreachable and unhealthy nodes are skipped.
    nodes: [
      "10.10.10.1:8123",
      "10.10.10.2:8123",
      "10.10.10.3:8123",
      "10.10.10.4:8123"
    ]
```
