---
title: Chproxy quick start
---

This quick start guide will walk you through an example of chproxy usage. After this guide, you will have seen how chproxy can be used to spread SELECT queries over a multi-node ClickHouse cluster.

To follow along with the quick start, clone [chproxy](https://github.com/ContentSquare/chproxy) from github and navigate to the `examples/quick-start` folder.

### Running the Quick Start

A docker-compose file is provided to run an example ClickHouse setup, with a multi-node ClickHouse setup.

Make sure your Docker Engine is provisioned with at least 4G of memory available. To start the ClickHouse servers, run:

```console
docker compose up
```

Optionally, to verify that the cluster is running properly, execute the following from a new terminal shell:

```console
docker exec -it quick_start_clickhouse_1 clickhouse-client --query "SELECT COUNT(*) FROM tutorial.views"
```

The ClickHouse server exposes an [HTTP interface](https://clickhouse.com/docs/en/interfaces/http/) on port 8123. Chproxy uses the HTTP interface to submit queries to Clickhouse.

The ClickHouse servers are already setup with a Distributed Table on each node, as described in [Spreading INSERTs](/use-cases/spread-insert).

The tables are set-up as follows. A local table is created on each node, which holds the data:

```sql
CREATE TABLE IF NOT EXISTS tutorial.views (
    datetime DateTime,
    uid UInt64,
    page String,
    views UInt32
)ENGINE = GenerateRandom(1, 5, 3);
```

> The GenerateRandom engine is used to automatically create some test data, saving the need to manually load in date for the tutorial.

Additionally on each node a distributed table is defined, referencing the local table:

```sql
CREATE TABLE IF NOT EXISTS tutorial.views_distributed AS tutorial.views
ENGINE = Distributed('clickhouse_cluster', tutorial, views, uid);
```

To stop the ClickHouse server and remove all data, run:

```console
docker compose down -v
```

### Querying the HTTP interface

We will use the following example query to query the data:

```sql
SELECT *
FROM tutorial.views 
LIMIT 1000 
FORMAT Pretty
```

To pass this query to Clickhouse directly, execute the following command:

```console
echo "SELECT * FROM tutorial.views LIMIT 1000 FORMAT Pretty" | curl 'http://localhost:8123/' --data-binary @-
```

This will execute the query on the node exposing the HTTP interface. ClickHouse internally routes the queries to the other nodes and gathers the results on the node being queried.

You can confirm this by looking at the logs in the docker-compose shell. If you repeat the command above you will see that the query is being executed at the same node:

```console
quick_start-clickhouse1-1  | 2023.01.12 19:17:32.129971 [ 453 ] {a51a5513-0f75-45e8-8e69-786cfbbbdf83} <Information> executeQuery: Read 1000 rows, 26.81 KiB in 0.024539749 sec., 40750 rows/sec., 1.07 MiB/sec.
quick_start-clickhouse1-1  | 2023.01.12 19:17:34.602913 [ 453 ] {e62994ac-5efb-4950-8e07-7e07f024d9cc} <Information> executeQuery: Read 1000 rows, 26.81 KiB in 0.289328989 sec., 3456 rows/sec., 92.65 KiB/sec.
quick_start-clickhouse1-1  | 2023.01.12 19:17:36.615416 [ 453 ] {cd861fa4-8711-40ab-a3a4-027f9b6b0517} <Information> executeQuery: Read 1000 rows, 26.81 KiB in 0.303865482 sec., 3290 rows/sec., 88.22 KiB/sec.
```

This can lead to situations where a single node is overloaded as it serves as the entry point for all queries on the distributed table. Additionally this leads to several underutilized nodes and one over utilized node, which isn’t efficient.

Chproxy can be utilized as a proxy in front of ClickHouse that will help balance the load. Chproxy will send the query to a different node each time the query is executed. This avoids the issues previously described.

To pass this query to chproxy directly, execute the following command:

```console
echo "SELECT * FROM tutorial.views LIMIT 1000 FORMAT Pretty" | curl 'http://default:password@localhost:9001/' --data-binary @-
```

Chproxy proxies the queries to the ClickHouse cluster and ensures an even balance of load for each of the ClickHouse nodes.

You can confirm this by looking at the logs in the docker-compose shell. If you repeat the command above you will see that the query is being executed on a different node each time:

```console
quick_start-clickhouse2-1  | 2023.01.12 19:20:49.925802 [ 236 ] {1739A57F2664DE54} <Information> executeQuery: Read 1000 rows, 26.81 KiB in 0.854256152 sec., 1170 rows/sec., 31.38 KiB/sec.
quick_start-clickhouse3-1  | 2023.01.12 19:20:52.036754 [ 238 ] {1739A57F2664DE55} <Information> executeQuery: Read 1000 rows, 26.81 KiB in 0.560763555 sec., 1783 rows/sec., 47.80 KiB/sec.
quick_start-clickhouse1-1  | 2023.01.12 19:20:53.599502 [ 237 ] {1739A57F2664DE56} <Information> executeQuery: Read 1000 rows, 26.81 KiB in 0.159569936 sec., 6266 rows/sec., 167.99 KiB/sec.
```

### Introducing query caching

Let's reduce the load on ClickHouse even more by caching queries. Often when building applications that utilise
ClickHouse, queries can be repeated many times. But the result won't change each time.
By caching the query response, we can limit the number of queries that are actually executed by ClickHouse.

To introduce a cache, we need to update the configuration of chproxy and define which caches should be used. The yaml snippet below highlights the configuration changes that are made. As you can see, we enable a file system cache that caches queries for 30s.

```yaml
users:
  - name: "default"
    ...
    cache: "default_cache"

...

caches:
  - name: "default_cache"
    mode: "file_system"
    file_system:
      dir: "/data/cache"
      max_size: 150Mb
    expire: 30s
    grace_time: 5s
```

We already prepared an example configuration in `examples/quick_start/resources/chproxy/config/config_with_cache.yml`. To run this example, just edit the chproxy service in the `docker-compose` file and update the command:

```yaml
version: '3'
services:
  ...
  chproxy:
    ...
    command: -config /config/config_with_cache.yml
    ...
```

Now when you run the docker compose stack again with `docker compose up` and execute the query from above again the first time you will see it hit the ClickHouse cluster. However, each execution of the query after for 30s will be cached. You can verify this by executing the command many times and checking the logs of ClickHouse to see if the query hit ClickHouse.
