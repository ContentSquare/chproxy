# chproxy [![Go Report Card](https://goreportcard.com/badge/github.com/Vertamedia/chproxy)](https://goreportcard.com/report/github.com/Vertamedia/chproxy)

Chproxy, is an http proxy for [ClickHouse](https://ClickHouse.yandex) database. It provides the following features:

- May proxy requests to multiple distinct `ClickHouse` clusters depending on the input user. For instance, requests from `appserver` user may go to `stats-raw` cluster, while requests from `reportserver` user may go to `stats-aggregate` cluster.
- May map input users to per-cluster users. This prevents from exposing real usernames and passwords used in `ClickHouse` clusters. Additionally this allows mapping multiple distinct input users to a single `ClickHouse` user.
- May accept incoming requests via HTTP and HTTPS.
- May limit HTTP and HTTPS access by IP/IP-mask lists.
- May limit per-user access by IP/IP-mask lists.
- May limit per-user query duration. Timed out queries are forcibly killed via [KILL QUERY](http://clickhouse-docs.readthedocs.io/en/latest/query_language/queries.html#kill-query).
- May limit per-user requests rate.
- May limit per-user number of concurrent requests.
- All the limits may be independently set for each input user and for each per-cluster user.
- Evenly spreads requests among cluster nodes using `least loaded` + `round robin` technique.
- Monitors node health and prevents from sending requests to unhealthy nodes.
- Supports automatic HTTPS certificate issuing and renewal via [Let’s Encrypt](https://letsencrypt.org/).
- May proxy requests to each configured cluster via either HTTP or [HTTPS](https://github.com/yandex/ClickHouse/blob/96d1ab89da451911eb54eccf1017eb5f94068a34/dbms/src/Server/config.xml#L15).
- Prepends User-Agent request header with remote/local address and in/out usernames before proxying it to `ClickHouse`, so this info may be queried from [system.query_log.http_user_agent](https://github.com/yandex/ClickHouse/issues/847).
- Exposes various useful [metrics](#metrics) in [prometheus text format](https://prometheus.io/docs/instrumenting/exposition_formats/).
- Configuration may be updated without restart - just send `SIGHUP` signal to `chproxy` process.
- Easy to manage and run - just pass config file path to a single `chproxy` binary.
- Easy to [configure](https://github.com/Vertamedia/chproxy/blob/master/config/examples/simple.yml):
```yml
server:
  http:
    listen_addr: ":9090"
    allowed_networks: ["127.0.0.0/24"]

users:
  - name: "default"
    to_cluster: "default"
    to_user: "default"

# by default each cluster has `default` user which can be overridden by section `users`
clusters:
  - name: "default"
    nodes: ["127.0.0.1:8123"]

```

## How to install

### Precompiled binaries

Precompiled `chproxy` binaries are available [here](https://github.com/Vertamedia/chproxy/releases).
Just download the latest stable binary, unpack and run it with the desired [config](#configuration):

```
./chproxy -config=/path/to/config.yml
```

### Building from source

Chproxy is written in [Go](https://golang.org/). The easiest way to install it from sources is:

```
go get -u github.com/Vertamedia/chproxy
```

If you don't have Go installed on your system - follow [this guide](https://golang.org/doc/install).


## Why it was created

`ClickHouse` may exceed [max_execution_time](http://clickhouse-docs.readthedocs.io/en/latest/settings/query_complexity.html#max-execution-time) and [max_concurrent_queries](https://github.com/yandex/ClickHouse/blob/add13f233eb6d30da4c75c4309542047a1dde033/dbms/src/Server/config.xml#L75) limits due to various reasons:
- `max_execution_time` may be exceeded due to the current [implementation deficiencies](https://github.com/yandex/ClickHouse/issues/217).
- `max_concurrent_queries` works only on a per-node basis. There is no way to limit the number of concurrent queries on a cluster if queries are spread across cluster nodes.

Such "leaky" limits may lead to high resource usage on all the cluster nodes. After facing this problem we had to maintain two distinct http proxies in front of our `ClickHouse` cluster - one for spreading `INSERT`s among cluster nodes and another one for sending `SELECT`s to a dedicated node where limits may be enforced somehow. This was fragile and inconvenient to manage, so `chproxy` has been created :)


## Use cases

### Spread `INSERT`s among cluster shards

Usually `INSERT`s are sent from app servers located in a limited number of subnetworks. `INSERT`s from other subnetworks must be denied.

All the `INSERT`s may be routed to a [distributed table](http://clickhouse-docs.readthedocs.io/en/latest/table_engines/distributed.html) on a single node. But this increases resource usage (CPU and network) on the node comparing to other nodes, since it must parse each row to be inserted and route it to the corresponding node (shard).

It would be better to spread `INSERT`s among available shards and to route them directly to per-shard tables instead of distributed tables. The routing logic may be embedded either directly into applications generating `INSERT`s or may be moved to a proxy. Proxy approach is better since it allows re-configuring `ClickHouse` cluster without modification of application configs and without application downtime. Multiple identical proxies may be started on distinct servers for scalability and availability purposes.

The following minimal `chproxy` config may be used for [this use case](https://github.com/Vertamedia/chproxy/blob/master/config/examples/spread.inserts.yml):
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

### Spread `SELECT`s from reporting apps among cluster nodes

Reporting servers are usually located in a limited number of subnetworks. Reporting apps usually generate various customer reports from `SELECT` query results. The load generated by such `SELECT`s on `ClickHouse` cluster may vary depending on the number of online customers and on the generated report types. It is obvious that the load must be limited in order to prevent cluster overload.

All the `SELECT`s may be routed to a [distributed table](http://clickhouse-docs.readthedocs.io/en/latest/table_engines/distributed.html) on a single node. But this increases resource usage (RAM, CPU and network) on the node comparing to other nodes, since it must do final aggregation, sorting and filtering for the data obtained from cluster nodes (shards).

It would be better to create identical distributed table on each shard and spread `SELECT`s among all the available shards.

The following minimal `chproxy` config may be used for [this use case](https://github.com/Vertamedia/chproxy/blob/master/config/examples/spread.selects.yml):
```yml
server:
  http:
      listen_addr: ":9090"

      # Networks with reporting servers.
      allowed_networks: ["10.10.2.0/24"]

users:
  - name: "report"
    to_cluster: "stats-aggregate"
    to_user: "readonly"
    max_concurrent_queries: 6
    max_execution_time: 1m

clusters:
  - name: "stats-aggregate"
    nodes: [
      "10.10.20.1:8123",
      "10.10.20.2:8123"
    ]
    users:
      - name: "readonly"
        password: "****"
```

### Authorize users by passwords via HTTPS

Suppose you need to access `ClickHouse` cluster from anywhere by username/password.
This may be used for building graphs from [ClickHouse-grafana](https://github.com/Vertamedia/ClickHouse-grafana) or [tabix](https://tabix.io/).
It is bad idea to transfer unencrypted password and data over untrusted networks.
So HTTPS must be used for accessing the cluster in such cases.
The following `chproxy` config may be used for [this use case](https://github.com/Vertamedia/chproxy/blob/master/config/examples/https.yml):
```yml
server:
  https:
    listen_addr: ":443"
    autocert:
      cache_dir: "certs_dir"

users:
  - name: "web"
    password: "****"
    to_cluster: "stats-raw"
    to_user: "web"
    max_concurrent_queries: 4
    max_execution_time: 30s
    requests_per_minute: 10
    deny_http: true

    # Allow `CORS` requests for `tabix`.
    allow_cors: true

clusters:
  - name: "stats-raw"
    nodes: [
     "10.10.10.1:8123",
     "10.10.10.2:8123",
     "10.10.10.3:8123",
     "10.10.10.4:8123"
    ]
    users:
      - name: "web"
        password: "****"
```

### All the above configs combined

All the above cases may be combined in a single `chproxy` [config](https://github.com/Vertamedia/chproxy/blob/master/config/examples/combined.yml):

```yml
server:
  http:
      listen_addr: ":9090"
      allowed_networks: ["10.10.1.0/24","10.10.2.0/24"]
  https:
    listen_addr: ":443"
    autocert:
      cache_dir: "certs_dir"

users:
  - name: "insert"
    allowed_networks: ["10.10.1.0/24"]
    to_cluster: "stats-raw"
    to_user: "default"

  - name: "report"
    allowed_networks: ["10.10.2.0/24"]
    to_cluster: "stats-aggregate"
    to_user: "readonly"
    max_concurrent_queries: 6
    max_execution_time: 1m

  - name: "web"
    password: "****"
    to_cluster: "stats-raw"
    to_user: "web"
    max_concurrent_queries: 4
    max_execution_time: 30s
    requests_per_minute: 10
    deny_http: true

clusters:
  - name: "stats-aggregate"
    nodes: [
      "10.10.20.1:8123",
      "10.10.20.2:8123"
    ]
    users:
    - name: "readonly"
      password: "****"

  - name: "stats-raw"
    nodes: [
     "10.10.10.1:8123",
     "10.10.10.2:8123",
     "10.10.10.3:8123",
     "10.10.10.4:8123"
    ]
    users:
      - name: "default"

      - name: "web"
        password: "****"
```

## Configuration

### Server
`Chproxy` may accept requests over `HTTP` and `HTTPS` protocols. [HTTPS](https://github.com/Vertamedia/chproxy/blob/master/config#https_config) must be configured with custom certificate or with automated [Let's Encrypt](https://letsencrypt.org/) certificates.

Access to `chproxy` can be limitied by list of IPs or IP masks. This option can be applied to [HTTP](https://github.com/Vertamedia/chproxy/blob/master/config#http_config), [HTTPS](https://github.com/Vertamedia/chproxy/blob/master/config#https_config), [metrics](https://github.com/Vertamedia/chproxy/blob/master/config#metrics_config), [user](https://github.com/Vertamedia/chproxy/blob/master/config#user_config) or [cluster-user](https://github.com/Vertamedia/chproxy/blob/master/config#cluster_user_config).

### Users
There are two types of users: `in-users` (in global section) and `out-users` (in cluster section).
This means all requests will be matched to `in-users` and if all checks are Ok - will be matched to `out-users`
with overriding credentials.

Suppose we have one ClickHouse user `web` with `read-only` permissions and `max_concurrent_queries: 4` limit.
There are two distinct applications `reading` from ClickHouse. We may create two distinct `in-users` with `to_user: "web"` and `max_concurrent_queries: 2` each in order to avoid situation when a single application exhausts all the 4-request limit on the `web` user.

Requests to `chproxy` must be authorized with credentials from [user_config](https://github.com/Vertamedia/chproxy/blob/master/config#user_config). Credentials can be passed via [BasicAuth](https://en.wikipedia.org/wiki/Basic_access_authentication) or via `user` and `password` [query string](https://en.wikipedia.org/wiki/Query_string) args.

Limits for `in-users` and `out-users` are independent.

### Clusters
`Chproxy` can be configured with multiple `cluster`s. Each `cluster` must have a name and a list of nodes.
Requests to each cluster are balanced using `round-robin` + `least-loaded` approach.
The node priority is automatically decreased for a short interval if recent requests to it were unsuccessful.
This means that the `chproxy` will choose the next least loaded healthy node for every new request.

Additionally each node is periodically checked for availability. Unavailable nodes are automatically excluded from the cluster until they become available again. This allows performing node maintenance without removing unavailable nodes from the cluster config.

`Chproxy` automatically kills queries exceeding `max_execution_time` limit. By default `chproxy` tries to kill such queries
under `default` user. The user may be overriden with [kill_query_user](https://github.com/Vertamedia/chproxy/blob/master/config#kill_query_user_config).


If `cluster`'s [users](https://github.com/Vertamedia/chproxy/blob/master/config#cluster_user_config) section isn't specified, then `default` user is used with no limits.

### Security
`Chproxy` removes all the query params from input requests (except the `query`) before proxying them to `ClickHouse` nodes. This prevents from unsafe overriding of various `ClickHouse` [settings](http://clickhouse-docs.readthedocs.io/en/latest/interfaces/http_interface.html).

Be careful when configuring limits, allowed networks, passwords etc.
By default `chproxy` tries detecting the most obvious configuration errors such as `allowed_networks: ["0.0.0.0/0"]` or sending passwords via unencrypted HTTP.

Special option `hack_me_please: true` may be used for disabling all the security-related checks during config validation (if you are feelying lucky :) ).

#### Example of [full](https://github.com/Vertamedia/chproxy/blob/master/config/testdata/full.yml) configuration:
```yml
# Whether to print debug logs.
log_debug: true

# Whether to ignore security checks during config parsing.
hack_me_please: true

# Cache configuration
caches:
  - name: "longterm"
    dir: "cache_dir"
    max_size: "10G"
    expire: "1h"

# Named network lists, might be used as values for `allowed_networks`.
network_groups:
  - name: "office"
    # Each item may contain either IP or IP subnet mask.
    networks: ["127.0.0.0/24", "10.10.0.1"]

  - name: "reporting-apps"
    networks: ["10.10.10.0/24"]

# Settings for `chproxy` input interfaces.
server:
  # Configs for input http interface.
  # The interface works only if this section is present.
  http:
    # TCP address to listen to for http.
    # May be in the form IP:port . IP part is optional.
    listen_addr: ":9090"

    # List of allowed networks or network_groups.
    # Each item may contain IP address, IP subnet mask or a name
    # from `network_groups`.
    # By default requests are accepted from all the IPs.
    allowed_networks: ["office", "reporting-apps", "1.2.3.4"]

  # Configs for input https interface.
  # The interface works only if this section is present.
  https:
    # TCP address to listen to for https.
    listen_addr: ":443"

    # Paths to TLS cert and key files.
    # cert_file: "cert_file"
    # key_file: "key_file"

    # Letsencrypt config.
    # Certificates are automatically issued and renewed if this section
    # is present.
    # There is no need in cert_file and key_file if this section is present.
    autocert:
      # Path to the directory where autocert certs are cached.
      cache_dir: "certs_dir"

      # The list of host names proxy is allowed to respond to.
      # See https://godoc.org/golang.org/x/crypto/acme/autocert#HostPolicy
      allowed_hosts: ["example.com"]

  # Metrics in prometheus format are exposed on the `/metrics` path.
  # Access to `/metrics` endpoint may be restricted in this section.
  # By default access to `/metrics` is unrestricted.
  metrics:
    allowed_networks: ["office"]

# Configs for input users.
users:
    # Name and password are used to authorize access via BasicAuth or
    # via `user`/`password` query params.
    # Password is optional. By default empty password is used.
  - name: "web"
    password: "****"

    # Requests from the user are routed to this cluster.
    to_cluster: "first cluster"

    # Input user is substituted by the given output user from `to_cluster`
    # before proxying the request.
    to_user: "web"

    # Whether to deny input requests over HTTP.
    deny_http: true

    # Whether to allow `CORS` requests like `tabix` does.
    # By default `CORS` requests are denied for security reasons.
    allow_cors: true

    # Requests per minute limit for the given input user.
    requests_per_minute: 4
    
    # Name of cache configuration to use
    cache: "longterm"

  - name: "default"
    to_cluster: "second cluster"
    to_user: "default"
    allowed_networks: ["office", "1.2.3.0/24"]

    # The maximum number of concurrently running queries for the user.
    max_concurrent_queries: 4

    # The maximum query duration for the user.
    # Timed out queries are forcibly killed via `KILL QUERY`.
    max_execution_time: 1m

    # Whether to deny input requests over HTTPS.
    deny_https: true

# Configs for ClickHouse clusters.
clusters:
    # The cluster name is used in `to_cluster`.
  - name: "first cluster"

    # Protocol to use for communicating with cluster nodes.
    # Currently supported values are `http` or `https`.
    # By default `http` is used.
    scheme: "http"

    # Cluster node addresses.
    # Requests are evenly distributed among them.
    nodes: ["127.0.0.1:8123", "shard2:8123"]

    # Each cluster node is checked for availability using this interval.
    # By default each node is checked for every 5 seconds.
    heartbeat_interval: 1m

    # Timed out queries are killed using this user.
    # By default `default` user is used.
    kill_query_user:
      name: "default"
      password: "***"

    # Configuration for cluster users.
    users:
        # The user name is used in `to_user`.
      - name: "web"
        password: "password"
        max_concurrent_queries: 4
        max_execution_time: 1m

  - name: "second cluster"
    scheme: "https"
    nodes: ["127.0.1.1:8123", "127.0.1.2:8123"]
    users:
      - name: "default"
        max_concurrent_queries: 4
        max_execution_time: 1m

      - name: "web"
        max_concurrent_queries: 4
        max_execution_time: 10s
        requests_per_minute: 10
        allowed_networks: ["office"]
```

#### Full specification is located [here](https://github.com/Vertamedia/chproxy/blob/master/config)

## Metrics
Metrics are exposed via [Prometheus](https://prometheus.io/) at `/metrics` path

| Name | Type | Description | Labels |
| ------------- | ------------- | ------------- | ------------- |
| status_codes_total | Counter | Distribution by response status codes | `user`, `cluster`, `cluster_user`, `cluster_node`, `code` |
| request_sum_total | Counter | The number of processed requests | `user`, `cluster`, `cluster_user`, `cluster_node` |
| request_success_total | Counter | The number of successfully proxied requests | `user`, `cluster`, `cluster_user`, `cluster_node` |
| request_duration_seconds | Summary | Request duration | `user`, `cluster`, `cluster_user`, `cluster_node` |
| concurrent_limit_excess_total | Counter | The number of rejected requests due to max_concurrent_queries limit | `user`, `cluster`, `cluster_user`, `cluster_node` |
| host_penalties_total | Counter | The number of given penalties by host | `cluster`, `cluster_node` |
| host_health | Gauge | Health state of hosts by clusters | `cluster`, `cluster_node` |
| concurrent_queries | Gauge | The number of concurrent queries at the moment | `user`, `cluster`, `cluster_user`, `cluster_node` |
| request_body_bytes_total | Counter | The amount of bytes read from request bodies | `user`, `cluster`, `cluster_user`, `cluster_node` |
| response_body_bytes_total | Counter | The amount of bytes written to response bodies | `user`, `cluster`, `cluster_user`, `cluster_node` |
| cache_hits_total | Counter | The amount of successful cache hits | `user`, `cluster`, `cluster_user`, `cluster_node` |
| cache_miss_total | Counter | The amount of cache miss | `user`, `cluster`, `cluster_user`, `cluster_node` |
| bad_requests_total | Counter | The number of unsupported requests | |

An example of [Grafana's](https://grafana.com) dashboard for `chproxy` metrics is available [here](https://github.com/Vertamedia/chproxy/blob/master/chproxy_overview.json)

![dashboard example](https://user-images.githubusercontent.com/2902918/31392734-b2fd4a18-ade2-11e7-84a9-4aaaac4c10d7.png)


## FAQ

* *Is `chproxy` production ready?*

  Yes, we successfully use it in production for both `INSERT` and `SELECT`
  requests.

* *What about `chproxy` performance?*

  A single `chproxy` instance easily proxies 1Gbps of compressed `INSERT` data
  while using less than 20% of a single CPU core in our production setup.

* *Does `chproxy` support [native interface](http://clickhouse-docs.readthedocs.io/en/latest/interfaces/tcp.html) for ClickHouse?*

  No. Because currently all our services work with ClickHouse only via HTTP.
  Support for `native interface` may be added in the future.
