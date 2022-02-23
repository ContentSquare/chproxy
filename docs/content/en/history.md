---
title: History
category: Community
position: 401
---

## Why it was created

`ClickHouse` may exceed [max_execution_time](http://clickhouse-docs.readthedocs.io/en/latest/settings/query_complexity.html#max-execution-time) and [max_concurrent_queries](https://github.com/yandex/ClickHouse/blob/add13f233eb6d30da4c75c4309542047a1dde033/dbms/src/Server/config.xml#L75) limits due to various reasons:
- `max_execution_time` may be exceeded due to the current [implementation deficiencies](https://github.com/yandex/ClickHouse/issues/217).
- `max_concurrent_queries` works only on a per-node basis. There is no way to limit the number of concurrent queries on a cluster if queries are spread across cluster nodes.

Such "leaky" limits may lead to high resource usage on all the cluster nodes. After facing this problem we had to maintain two distinct HTTP proxies in front of our `ClickHouse` cluster - one for spreading `INSERT`s among cluster nodes and another one for sending `SELECT`s to a dedicated node where limits may be enforced somehow. This was fragile and inconvenient to manage, so `chproxy` has been created :)

