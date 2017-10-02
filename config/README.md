# chproxy [![Go Report Card](https://goreportcard.com/badge/github.com/Vertamedia/chproxy)](https://goreportcard.com/report/github.com/Vertamedia/chproxy)

TODO:
- billions of connections
- hack me plz
- rate limit explanation
- add dashboard for monitoring
- renew metrics description and explain how to use them
- mention about URL params purifying



Chproxy, is an http proxy for [ClickHouse](https://ClickHouse.yandex) database. It provides the following features:

- May proxy requests to multiple distinct `ClickHouse` clusters depending on the input user. For instance, requests from `appserver` user may go to `stats-raw` cluster, while requests from `reportserver` user may go to `stats-aggregate` cluster.
- May map input users to per-cluster users. This prevents from exposing real users and passwords used in `ClickHouse` clusters.
- May limit access to `chproxy` via IP/IP-mask lists.
- May independently limit access for each input user via IP/IP-mask lists.
- May limit per-user query duration. Timed out queries are forcibly killed via `KILL QUERY` request.
- May limit per-user requests per minute amount.
- May limit per-user number of concurrent requests.
- Query duration, concurrent requests and request per minute limits may be independently set for each input user and for each per-cluster user.
- Evenly spreads requests among cluster nodes using `least loaded` + `round robin` technique.
- Monitors node health and prevents from sending requests to unhealthy nodes.
- May accept incoming requests via either HTTP or HTTPS.
- Supports automatic HTTPS certificate issuing and renewal via [Let’s Encrypt](https://letsencrypt.org/).
- May proxy requests to each configured cluster via either HTTP or HTTPS.
- Prepends User-Agent request header with remote/local ip and input username before proxying it to `ClickHouse`, so this info may be queried from `system.query_log.http_user_agent`.
- Exposes [metrics](#metrics) in [prometheus text format](https://prometheus.io/docs/instrumenting/exposition_formats/).
- Configuration may be updated without restart - just send `SIGHUP` signal to `chproxy` process.
- Easy to manage and run - just pass config file path to a single `chproxy` binary.
- Easy to configure:
```yml
server:
  http:
      listen_addr: ":9090"
      allowed_networks: ["127.0.0.1/24"]

users:
  - name: "default"
    to_cluster: "default"
    to_user: "default"

# by default each cluster has user `default` which can be overridden by section `users`
clusters:
  - name: "default"
    nodes: ["127.0.0.1:8123"]

```

## Why it was created

We faced a situation when `ClickHouse` exceeded `max_execution_time` and `max_concurrent_queries` limits due to various reasons:
- `max_execution_time` may be exceeded due to the current implementation deficiencies.
- `max_concurrent_queries` works only on a per-node basis. There is no way to limit the number of concurrent queries on a cluster if queries are spread across cluster nodes.

This led to high resource usage on all the cluster nodes. We had to kill those queries manually (since `ClickHouse` didn't kill them by itself) and to launch a dedicated http proxy for sending all the requests to a dedicated `ClickHouse` node under the given user. Now we had two distinct http proxies in front of our `ClickHouse` cluster - one for spreading `INSERTs` among cluster nodes and another one for sending `SELECTs` to a dedicated node where limits may be enforced. This was fragile and inconvenient to manage, so `chproxy` has been created :)


## Use cases

### Spread `INSERTs` among cluster shards

Usually `INSERTs` are sent from application servers located in a limited number of subnetworks. `INSERTs` from other subnetworks must be denied.

All the `INSERTs` may be routed to a distributed table on a single node. But this increases resource usage (CPU and network) on the node comparing to other nodes, since it must parse each row from each `INSERT` and route it to the corresponding node (shard).

It would be better to spread `INSERTs` among available shards and to route them directly to per-shard tables instead of distributed tables. The routing logic may be embedded either directly into applications generating `INSERTs` or may be moved to a proxy. Proxy approach is better since it allows re-configuring `ClickHouse` cluster without modification of application configs and without application downtime.

The following minimal `chproxy` config may be used for this use case:
```yml
server:
  http:
      listen_addr: ":9090"
      allowed_networks: ["10.10.10.1/24"]

users:
  - name: "insert"
    to_cluster: "stats-raw"
    to_user: "default"

clusters:
  - name: "stats-raw"
#  requests will be spread in `round-robin` + `least-loaded` manner among nodes
    nodes: [
      "10.10.10.1:8123",
      "10.10.10.2:8123",
      "10.10.10.3:8123",
      "10.10.10.4:8123"
    ]
```

### Spread `SELECTs` from reporting apps among cluster nodes

Reporting app servers are usually located in a limited number of subnetworkds. Reporting apps usually generate various customer reports from `SELECT` query results. The load generated by such `SELECTs` on `ClickHouse` cluster may vary depending on the number of online customers and on the generated reports. It is obvious that the load must be limited in order to prevent cluster overload.

All the `SELECTs` may be routed to a distributed table on a single node. But this increases resource usage (RAM, CPU and network) on the node comparing to other nodes, since it must do final aggregation, sorting and filtering for the data obtained from nodes (shards).

It would be better to create identical distributed table on each shard and spread `SELECTs` among all the available shards.

The following minimal `chproxy` config may be used for this use case:
```yml
server:
  http:
      listen_addr: ":9090"
      allowed_networks: ["127.0.0.1/24"]

users:
  - name: "report"
    to_cluster: "stats-aggregate"
    to_user: "readonly"
    max_concurrent_queries: 6
    max_execution_time: 1m

clusters:
  - name: "stats-aggregate"
    nodes: [
      "10.10.0.1:8123",
      "10.10.0.2:8123"
    ]
    users:
      - name: "readonly"
        password: "****"
```

### Authorize users by passwords via HTTPS

Suppose you need to access `ClickHouse` cluster from anywhere by username/password. This may be used for building graphs from [ClickHouse-grafana](https://github.com/Vertamedia/ClickHouse-grafana). It is bad idea to transfer unencrypted password and data over untrusted networks. So HTTPS must be used for accessing the cluster in such cases. The following `chproxy` config may be used for this use case:
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

All the above cases may be combined in a single `chproxy` config:

```yml
server:
  http:
      listen_addr: ":9090"
      allowed_networks: ["10.10.10.1/24"]
  https:
    listen_addr: ":443"
    autocert:
      cache_dir: "certs_dir"

users:
  - name: "insert"
    to_cluster: "stats-raw"
    to_user: "default"
    deny_https: true

  - name: "web"
    password: "****"
    to_cluster: "stats-raw"
    to_user: "web"
    max_concurrent_queries: 4
    max_execution_time: 30s
    requests_per_minute: 10
    deny_http: true

  - name: "report"
    to_cluster: "stats-aggregate"
    to_user: "readonly"
    max_concurrent_queries: 6
    max_execution_time: 1m
    deny_https: true

clusters:
  - name: "stats-aggregate"
    nodes: [
      "10.10.0.1:8123",
      "10.10.0.2:8123"
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
Chproxy allows to setup web-proxy with `HTTP` or `HTTPS` protocols. [HTTPS](#https_config) might be configured with
custom certificate or with automated [Let's Encrypt](https://letsencrypt.org/) certificates.

Access to proxy can be [limitied](#networks) by list of IPs or IP masks. This option can be applied to [HTTP](#http_config), [HTTPS](#https_config), [metrics](#metrics_config), [user](#user_config) or [cluster-user](#cluster_user_config).

### Users
There are two types of users: `in-users` (in global section) and `out-users` (in cluster section).
This means all requests will be matched to `in-users` and if all checks are Ok - will be matched to `out-users`
with overriding credentials.


For example, we have one ClickHouse user `web` with `read-only` permissions and limits `max_concurrent_queries=4`.
And two applications which are `reading` from ClickHouse. So we are creating two `in-users` with `max_concurrent_queries=2` and `to_user=web`.
This will help to avoid situation when one application will use all 4-request limit.


All requests to CHProxy must be authorized with credentials from [user_config](#user_config). Credentials can be passed
via BasicAuth or via URL `user` and `password` params. Limits for `in-users` and `out-users` are independent.

### Clusters
Proxy can be configured with multiple `cluster`s. Each `cluster` must have a name and a list of nodes.
All requests to cluster will be balanced with `round-robin` and `least-loaded` way.
If some of requests to ClickHouse node were unsuccessful - this node priority will be decreased for short period.
It means that proxy will chose next least loaded healthy node for every new request.


There is also `heartbeat_interval` which is just checking all nodes for availability. If node is unavailable it will be excluded
from the list until connection will be restored. Such behavior must help to reduce number of unsuccessful requests in case of network lags.


If some of proxied queries through cluster will run out of `max_execution_time` limit, proxy will try to kill them.
But this is possible only if `cluster` configured with [kill_query_user](#kill_query_user_config)


If `cluster`'s [users](#cluster_user_config) are not specified, it means that there is only a "default" user with no limits.


Example of full configuration can be found [here](https://github.com/Vertamedia/chproxy/blob/master/config/testdata/full.yml) or simplest [here](https://github.com/Vertamedia/chproxy/blob/master/config/testdata/default_values.yml)

### Possible types used in configuration:

 - `<bool>`: a boolean value `true` or `false`
 - `<addr>`: string value consisting of a hostname or IP followed by an optional port number
 - `<scheme>`: a string that can take the values `http` or `https`
 - `<duration>`: a duration matching the regular expression `[0-9]+(ms|[smhdwy])`
 - `<networks>`: string value consisting of IP, IP mask or named group, for example `"127.0.0.1"` or `"127.0.0.1/24"`
 - `<host_name>`: string value consisting of host name, for example `"example.com"`

Example of full configuration:
```yml
log_debug: true

network_groups:
  - name: "office"
    networks: ["127.0.0.1/24"]

server:
  http:
    listen_addr: ":9090"
    allowed_networks: ["office"]

  https:
    listen_addr: ":443"
    autocert:
      cache_dir: "certs_dir"
      allowed_hosts: ["example.com"]

  metrics:
    allowed_networks: ["office"]

users:
  - name: "web"
    password: "password"
    to_cluster: "second cluster"
    to_user: "web"
    deny_http: true
    requests_per_minute: 4

  - name: "default"
    to_cluster: "second cluster"
    to_user: "default"
    allowed_networks: ["office", "1.2.3.0/24"]
    max_concurrent_queries: 4
    max_execution_time: 1m
    deny_https: true

clusters:
  - name: "first cluster"
    scheme: "http"
    nodes: ["127.0.0.1:8123", "127.0.0.2:8123", "127.0.0.3:8123"]
    heartbeat_interval: 1m
    kill_query_user:
      name: "default"
      password: "password"
    users:
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
```

Global configuration consist of:
```yml
# Whether to print debug logs
log_debug: <bool> | default = false [optional]

# Whether to ignore security warnings
hack_me_please bool | default = false [optional]

# Named network lists
network_groups: <network_groups_config> ... [optional]

server:
  <server_config> [optional]

# List of allowed users
# which requests will be proxied to ClickHouse
users:
  - <user_config> ...

clusters:
  - <cluster_config> ...
```

### <network_groups_config>
```yml
# Name of group
name: "office"

# List of networks access is allowed from
# Each list item could be IP address or subnet mask
networks: <networks> ...
```

### <server_config>
```yml
# HTTP server configuration
http: <http_config> [optional]

# HTTPS server configuration
https: <https_config> [optional]

# Metrics handler configuration
metrics: <metrics_config> [optional]
```

#### <http_config>
```yml
# TCP address to listen to for http
listen_addr: <addr>

# List of networks or network_groups access is allowed from
# Each list item could be IP address or subnet mask
allowed_networks: <network_groups>, <networks> ... | optional
```

#### <https_config>
```yml
# TCP address to listen to for https
listen_addr: <addr> | optional | default = `:443`

# List of networks or network_groups access is allowed from
# Each list item could be IP address or subnet mask
allowed_networks: <network_groups>, <networks> ... | optional

# Certificate and key files for client cert authentication to the server
cert_file: <string> | optional
key_file: <string> | optional

# Autocert configuration via letsencrypt
autocert: <autocert_config> | optional
```

#### <autocert_config>
```yml
# Path to the directory where autocert certs are cached
cache_dir: <string>

# List of host names to which proxy is allowed to respond to
# see https://godoc.org/golang.org/x/crypto/acme/autocert#HostPolicy
allowed_hosts: <host_name> ... | optional
```

#### <metrics_config>
```yml
# List of networks or network_groups access is allowed from
# Each list item could be IP address or subnet mask
allowed_networks: <network_groups>, <networks> ... | optional
```

### <user_config>
```yml
# User name, will be taken from BasicAuth or from URL `user`-param
name: <string>

# User password, will be taken from BasicAuth or from URL `password`-param
password: <string> | optional

# Must match with name of `cluster` config,
# where requests will be proxied
to_cluster: <string>

# Must match with name of `user` from `cluster` config,
# whom credentials will be used for proxying request to CH
to_user: <string>

# Maximum number of concurrently running queries for user
max_concurrent_queries: <int> | optional | default = 0

# Maximum duration of query execution for user
max_execution_time: <duration> | optional | default = 0

# Maximum number of requests per minute for user
requests_per_minute: <int> | optional | default = 0

# Whether to deny http connections for this user
deny_http: <bool> | optional | default = false

# Whether to deny https connections for this user
deny_https: <bool> | optional | default = false

# List of networks or network_groups access is allowed from
# Each list item could be IP address or subnet mask
allowed_networks: <network_groups>, <networks> ... | optional
```

### <cluster_config>
```yml
# Name of CH cluster, must match with `to_cluster`
name: <string>

# Scheme: `http` or `https`; would be applied to all nodes
scheme: <scheme> | optional | default = "http"

# List of nodes addresses. Requests would be balanced among them
nodes: <addr> ...

# List of ClickHouse cluster users
users:
    - <cluster_user_config> ...

# KillQueryUser - user configuration for killing
# queries which has exceeded limits
# if not specified - killing queries will be omitted
kill_query_user: <kill_query_user_config> | optional

# An interval for checking all cluster nodes for availability
heartbeat_interval: <duration> | optional | default = 5s
```

#### <cluster_user_config>
```yml
# User name in ClickHouse `users.xml` config
name: <string>

# User password in ClickHouse `users.xml` config
password: <string> | optional 

# Maximum number of concurrently running queries for user
max_concurrent_queries: <int> | optional | default = 0

# Maximum duration of query execution for user
max_execution_time: <duration> | optional | default = 0

# Maximum number of requests per minute for user
requests_per_minute: <int> | optional | default = 0
```

### <kill_query_user_config>
```yml
# User name to access CH with basic auth
name: <string>

# User password to access CH with basic auth
password: <string> | optional
```

## Metrics
Metrics are exposed via [Prometheus](https://prometheus.io/) at `/metrics` path

| Name | Type | Description | Labels |
| ------------- | ------------- | ------------- | ------------- |
| status_codes_total | Counter | Distribution by status codes | `user`, `cluster_user`, `host`, `code` |
| request_sum_total | Counter | Total number of sent requests | `user`, `cluster_user`, `host` |
| request_success_total | Counter | Total number of sent success requests | `user`, `cluster_user`, `host` |
| request_duration_seconds | Summary | Request duration | `user`, `cluster_user`, `host` |
| concurrent_limit_excess_total | Counter | Total number of max_concurrent_queries excess | `user`, `cluster_user`, `host` |
| host_penalties_total | Counter | Total number of given penalties by host | `user`, `cluster_user`, `host` |
| host_health | Gauge | Health state of hosts by clusters | `cluster_user`, `host` |
| good_requests_total | Counter | Total number of proxy requests | |
| bad_requests_total | Counter | Total number of unsupported requests | |

