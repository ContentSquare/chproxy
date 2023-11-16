---
title: History
---

## Why it was created

`ClickHouse` may exceed [max_execution_time](https://clickhouse.com/docs/en/operations/settings/query-complexity/#max-execution-time) and [max_concurrent_queries](https://clickhouse.com/docs/en/operations/server-configuration-parameters/settings/#max-concurrent-queries) limits due to various reasons:
- `max_execution_time` may be exceeded due to the current [implementation deficiencies](https://github.com/yandex/ClickHouse/issues/217).
- `max_concurrent_queries` works only on a per-node basis. There is no way to limit the number of concurrent queries on a cluster if queries are spread across cluster nodes.

Such "leaky" limits may lead to high resource usage on all the cluster nodes. After facing this problem we had to maintain two distinct HTTP proxies in front of our `ClickHouse` cluster - one for spreading `INSERT`s among cluster nodes and another one for sending `SELECT`s to a dedicated node where limits may be enforced somehow. This was fragile and inconvenient to manage, so `chproxy` has been created :)

