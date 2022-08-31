---
title: Chproxy
description: Chproxy is an http proxy and load balancer for ClickHouse
---

[![Go Report Card](https://goreportcard.com/badge/github.com/ContentSquare/chproxy)](https://goreportcard.com/report/github.com/ContentSquare/chproxy)
[![Build Status](https://travis-ci.org/ContentSquare/chproxy.svg?branch=master)](https://travis-ci.org/ContentSquare/chproxy?branch=master)
[![Coverage](https://img.shields.io/badge/gocover.io-75.7%25-green.svg)](http://gocover.io/github.com/ContentSquare/chproxy?version=1.9)

# chproxy

Chproxy 是一个用于 [ClickHouse](https://clickhouse.tech) 数据库的 HTTP 代理、负载均衡器。具有以下特性：

* 支持根据输入用户代理请求到多个 `ClickHouse` 集群。比如，把来自 `appserver` 的用户请求代理到 `stats-raw`  集群，把来自 `reportserver` 用户的请求代理到 `stats-aggregate` 集群。
* 支持将输入用户映射到每个 ClickHouse 实际用户，这能够防止暴露 ClickHouse 集群的真实用户名称、密码信息。此外，chproxy 还允许映射多个输入用户到某一个单一的 ClickHouse 实际用户。
* 支持接收 HTTP 和 HTTPS 请求。
* 支持通过 IP 或 IP 掩码列表限制 HTTP、HTTPS 访问。
* 支持通过 IP 或 IP 掩码列表限制每个用户的访问。
* 支持限制每个用户的查询时间，通过 [KILL QUERY](https://clickhouse.tech/docs/en/sql-reference/statements/kill/#kill-query-statement) 强制杀执行超时或者被取消的查询。
* 支持限制每个用户的请求频率。
* 支持限制每个用户的请求并发数。
* 所有的限制都可以对每个输入用户、每个集群用户进行设置。
* 支持自动延迟请求，直到满足对用户的限制条件。
* 支持配置每个用户的[响应缓存](/configuration/caching)。
* 响应缓存具有内建保护功能，可以防止 [惊群效应（thundering herd）](https://en.wikipedia.org/wiki/Cache_stampede)，即 dogpile 效应。
* 通过 `least loaded` 和 `round robin` 技术实现请求在副本和节点间的均衡负载。
* 支持检查节点健康情况，防止向不健康的节点发送请求。
* 通过 [Let’s Encrypt](https://letsencrypt.org/) 支持 HTTPS 自动签发和更新。
* 可以自行指定选用 HTTP 或 HTTPS 向每个配置的集群代理请求。
* 在将请求代理到 `ClickHouse` 之前，预先将 User-Agent 请求头与远程/本地地址，和输入/输出的用户名进行关联，因此这些信息可以在  [system.query_log.http_user_agent](https://github.com/yandex/ClickHouse/issues/847) 中查询到。
* 暴露各种有用的符合 [Prometheus](https://prometheus.io/docs/instrumenting/exposition_formats/) 内容格式的[指标（metrics）](/configuration/metrics)。
* 支持配置热更新，配置变更无需重启 —— 只需向 `chproxy`  进程发送一个 `SIGHUP` 信号即可。
* 易于管理和运行 —— 只需传递一个配置文件路径给 `chproxy` 即可。
* 易于[配置](https://github.com/ContentSquare/chproxy/blob/master/config/examples/simple.yml):


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

## 如何安装

### 使用预编译的二进制文件

可以从[此处](https://github.com/ContentSquare/chproxy/releases)下载预编译的 `chproxy` 二进制文件。

只需要下载最新的稳定版二进制文件，解压使用所需要的[配置](/configuration/default)运行。

```
./chproxy -config=/path/to/config.yml
```

### 从源码进行编译

Chproxy 是基于 [Go](https://golang.org/) 开发的，最简单的方式是如下的源编译安装：

```
go get -u github.com/ContentSquare/chproxy
```

如果你的系统没有安装 Go，可以参考这个[操作指南](https://golang.org/doc/install)。


## 为什么开发了Chproxy

由于各种原因，ClickHouse 的最大执行时间、最大并发语句可能会超过 [max_execution_time](https://clickhouse.com/docs/en/operations/settings/query-complexity/#max-execution-time) 和[max_concurrent_queries](https://clickhouse.com/docs/en/operations/server-configuration-parameters/settings/#max-concurrent-queries) 的限制：

* `max_execution_time`  可能会因为当前实现的缺陷而被超过。
* `max_concurrent_queries`  只针对每个节点的限制。如果是在集群节点上，是没法限制集群整体的并发查询数量。

这种 “泄漏” 的限制可能会导致所有集群节点的高资源使用率。遇到这个问题后，我们不得不在我们的 ClickHouse 集群前维护 2 个不同的 http 代理 —— 一个用于在集群节点间分散 `INSERT` 操作，另一个用于发送 `SELECT` 到一个专用节点，在该节点再通过某种方式进行限制。这样很不健壮，管理起来也很不方便，所以我们开发了 Chproxy。：）


## 使用示例

### 在集群分片间分散 `INSERT`

 通常 `INSERT` 操作是由有限的几个子网络中的应用服务器发送的，来自其他子网络的 `INSERT` 操作必须被拒绝。

所有的 `INSERT` 操作可能会被路由到一个节点上的[分布式表](https://clickhouse.tech/docs/en/engines/table-engines/special/distributed/)，但与其他节点对比，会增加该节点上的资源使用（CPU 和网络资源），因为该节点必须解析每一条被插入的记录，并路由到对应的节点分片。

所以最好是将 `INSERT` 分散到可用的分片节点上，并将其路由到每个分片表，而不是直接向分布式表进行请求。路由逻辑可以直接嵌入到应用程序生成分散后的 `INSERT` ，或者通过代理请求的方式。代理请求的方式会更好一些，因为它允许在不修改应用程序配置，对 `ClickHouse` 集群进行重新配置，避免应用程序的停机时间。多个相同的代理服务可以在不同的服务器上运行，以达到可拓展、高可用的目的。

以下是[INSERT分散示例](https://github.com/ContentSquare/chproxy/blob/master/config/examples/spread.inserts.yml)的最小 `chproxy` 配置：

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

### 在集群节点间分散来自报表应用的 `SELECT` 

报表应用通常会从 `SELECT` 查询结果中产生各种用户报表。这些 `SELECT` 产生的负载在 `ClickHouse` 集群上可能会有所不同，这取决于在线用户数量和生成的报表类型。很明显，为了防止集群过载，必须限制负载。

所有的 `SELECT` 可能会被路由到单个节点上的[分布式表](https://clickhouse.tech/docs/en/engines/table-engines/special/distributed/)，该节点会有比其他节点更高的资源使用量（RAM、CPU、网络），因为它必须对从集群其他分片节点获取到数据进行聚合、排序、过滤。

最好是在每个分片节点上创建相同的分布式表，并将 `SELECT` 分散到所有可用的分片节点上。

以下是[SELECT分散示例](https://github.com/ContentSquare/chproxy/blob/master/config/examples/spread.selects.yml)的最小 `chproxy` 配置：

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

### 通过 HTTPS 进行用户密码认证

假如你需要设置用户名称/密码用于在任何地方访问 `ClickHouse` 集群，可能用于在 [ClickHouse-grafana](https://github.com/Altinity/clickhouse-grafana) 或 [Tabix](https://tabix.io/) 创建图形界面管理。通过不信任的网络传输为加密的密码和数据，是一个坏主意。因此在这种情况下，必须通过 HTTPS 访问集群。

以下的 `chproxy` 配置示例演示了 [HTTPS 配置](https://github.com/ContentSquare/chproxy/blob/master/config/examples/https.yml)：

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
    max_concurrent_queries: 2
    max_execution_time: 30s
    requests_per_minute: 10
    deny_http: true

    # Allow `CORS` requests for `tabix`.
    allow_cors: true

    # Enable requests queueing - `chproxy` will queue up to `max_queue_size`
    # of incoming requests for up to `max_queue_time` until they stop exceeding
    # the current limits.
    # This allows gracefully handling request bursts when more than
    # `max_concurrent_queries` concurrent requests arrive.
    max_queue_size: 40
    max_queue_time: 25s

    # Enable response caching. See cache config below.
    cache: "shortterm"

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

caches:
  - name: "shortterm"
    dir: "/path/to/cache/dir"
    max_size: 150Mb

    # Cached responses will expire in 130s.
    expire: 130s
```

### 以上配置的组合

以上的配置可以通过一个单独的 `chproxy` [config](https://github.com/ContentSquare/chproxy/blob/master/config/examples/combined.yml) 配置文件进行组合：

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
    max_concurrent_queries: 2
    max_execution_time: 30s
    requests_per_minute: 10
    deny_http: true
    allow_cors: true
    max_queue_size: 40
    max_queue_time: 25s
    cache: "shortterm"

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

caches:
  - name: "shortterm"
    dir: "/path/to/cache/dir"
    max_size: 150Mb
    expire: 130s
```

## 配置

### 服务配置
`Chproxy`  可以接收 HTTP 和 HTTPS 协议的请求。其中 [HTTPS](https://github.com/ContentSquare/chproxy/tree/master/config#https_config) 必须使用自定义证书或自动 [Let's Encrypt](https://letsencrypt.org) 证书进行配置。

可以通过 IP 列表或 IP 掩码进行限制对  `chproxy`  的访问进行限制。这个选项可以应用于  [HTTP](https://github.com/ContentSquare/chproxy/blob/master/config#http_config), [HTTPS](https://github.com/ContentSquare/chproxy/blob/master/config#https_config), [metrics](https://github.com/ContentSquare/chproxy/blob/master/config#metrics_config), [user](https://github.com/ContentSquare/chproxy/blob/master/config#user_config) 或 [cluster-user](https://github.com/ContentSquare/chproxy/blob/master/config#cluster_user_config) 配置。

### 用户配置
有两种类型的用户： `in-users` (在全局部分) 和 `out-users` (在集群部分)。

所有的请求都会匹配到  `in-users` ，如果所有检查通过，将会被覆盖凭证后匹配到 `out-users`。

假设我们有一个只读权限 `read-only` 的 `ClickHouse` 用户 `web` ，和 `max_concurrent_queries: 4`  的最高并发限制。

有两个不同的应用程序从 `ClickHouse` 读取，我们可以创建两个不同的 `in-users` ，对应的 `to_user`  为 “web”，同时为每一个都设置 `max_concurrent_queries: 2`，以避免任意一个应用用尽所有 web 用户的 4 并发请求限制。

对 `chproxy`的请求必须通过 [user_config](https://github.com/ContentSquare/chproxy/blob/master/config#user_config) 配置中的用户凭证认证，凭证信息可以通过 [BasicAuth](https://en.wikipedia.org/wiki/Basic_access_authentication) 或用户名/密码的查询字符串参数来传递。

对于  `in-users` 和 `out-users` 的限制是相互独立的。

### 集群配置
`Chproxy` 可以配置多个逻辑集群 `cluster`，每个逻辑集群必须包含一个名称和节点列表、或者副本节点列表。请参考 [cluster-config](https://github.com/ContentSquare/chproxy/tree/master/config#cluster_config) 了解更详细的内容。

对集群节点、副本节点之间的请求采用的是 `round-robin` + `least-loaded` 的均衡方式。

如果在近期对某个节点请求不成功，该节点在很短的时间间隔内优先度会被自动降低。这意味着 `chproxy` 在每次请求中，会自动选择副本负载最小的健康节点。

此外，每个节点都会定期检查可用性。不可用的节点会被自动从逻辑集群中排除，直到它们再次可用为止。这允许在不从实际的 ClickHouse 集群中删除不可用节点的情况下，进行节点维护。

`Chproxy` 会自动杀死超过 `max_execution_time` 限制的查询。默认情况下，`chproxy` 会尝试杀死  `default` 用户下的这些超时查询，可以通过 [kill_query_user](https://github.com/ContentSquare/chproxy/blob/master/config#kill_query_user_config) 来覆盖该用户设置。

如果没有指定逻辑集群的[用户配置](https://github.com/ContentSquare/chproxy/blob/master/config#cluster_user_config)部分，会默认使用 `default` 用户。

### 缓存配置

`Chproxy`  支持配置响应缓存。可以创建多个 [cache-configs](https://github.com/ContentSquare/chproxy/blob/master/config/#cache_config) 中的各项细节配置。

通过给用户指定缓存名称，可以启用响应缓存。多个用户可以共享同一个缓存。

目前只有  `SELECT`  响应会被缓存。

对于请求字符串中设置了  `no_cache=1` 的请求，缓存会被禁用。


可以在查询字符串中传递可选的缓存命名空间，如  `cache_namespace=aaaa` 。这允许缓存在不同命名空间下对相同的查询做出不同的响应。此外，可以在缓存空间上建立即时缓存刷新 —— 只需按照顺序切换到新的命名空间，即可刷新缓存。

### 安全配置
`Chproxy` 可以在将请求代理到 ClickHouse 集群前，自定义从输入请求中删除查询参数（除了这里列出的[用户参数](https://github.com/ContentSquare/chproxy/blob/master/config#param_groups_config) 和[这些参数](https://github.com/ContentSquare/chproxy/blob/master/scope.go#L292)）。这可以防止请求不安全地覆盖各种 ClickHouse [设置](https://clickhouse.tech/docs/en/interfaces/http/#cli-queries-with-parameters)。

在设置限制时候需要小心，比如允许网络、密码等。

默认情况下，`chproxy` 会尝试检测最明显的安全配置问题时，如 `allowed_networks: ["0.0.0.0/0"]` ，或未加密的 HTTP 发送密码行为。

特殊选项 `hack_me_please: true` 可以用于禁用 chproxy 对配置的安全相关检查。（如果你感觉良好 ：））



#### 完整配置[示例](https://github.com/ContentSquare/chproxy/blob/master/config/testdata/full.yml)

```yml
# Whether to print debug logs.
#
# By default debug logs are disabled.
log_debug: true

# Whether to ignore security checks during config parsing.
#
# By default security checks are enabled.
hack_me_please: true

# Optional response cache configs.
#
# Multiple distinct caches with different settings may be configured.
caches:
    # Cache name, which may be passed into `cache` option on the `user` level.
    #
    # Multiple users may share the same cache.
  - name: "longterm"

    # Path to directory where cached responses will be stored.
    dir: "/path/to/longterm/cachedir"

    # Maximum cache size.
    # `Kb`, `Mb`, `Gb` and `Tb` suffixes may be used.
    max_size: 100Gb

    # Expiration time for cached responses.
    expire: 1h

    # When multiple requests with identical query simultaneously hit `chproxy`
    # and there is no cached response for the query, then only a single
    # request will be proxied to clickhouse. Other requests will wait
    # for the cached response during this grace duration.
    # This is known as protection from `thundering herd` problem.
    #
    # By default `grace_time` is 5s. Negative value disables the protection
    # from `thundering herd` problem.
    grace_time: 20s

  - name: "shortterm"
    dir: "/path/to/shortterm/cachedir"
    max_size: 100Mb
    expire: 10s

# Optional network lists, might be used as values for `allowed_networks`.
network_groups:
  - name: "office"
    # Each item may contain either IP or IP subnet mask.
    networks: ["127.0.0.0/24", "10.10.0.1"]

  - name: "reporting-apps"
    networks: ["10.10.10.0/24"]

# Optional lists of query params to send with each proxied request to ClickHouse.
# These lists may be used for overriding ClickHouse settings on a per-user basis.
param_groups:
    # Group name, which may be passed into `params` option on the `user` level.
  - name: "cron-job"
    # List of key-value params to send
    params:
      - key: "max_memory_usage"
        value: "40000000000"

      - key: "max_bytes_before_external_group_by"
        value: "20000000000"

  - name: "web"
    params:
      - key: "max_memory_usage"
        value: "5000000000"

      - key: "max_columns_to_read"
        value: "30"

      - key: "max_execution_time"
        value: "30"

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

    # ReadTimeout is the maximum duration for proxy to reading the entire
    # request, including the body.
    # Default value is 1m
    read_timeout: 5m

    # WriteTimeout is the maximum duration for proxy before timing out writes of the response.
    # Default is largest MaxExecutionTime + MaxQueueTime value from Users or Clusters
    write_timeout: 10m

    # IdleTimeout is the maximum amount of time for proxy to wait for the next request.
    # Default is 10m
    idle_timeout: 20m

  # Configs for input https interface.
  # The interface works only if this section is present.
  https:
    # TCP address to listen to for https.
    listen_addr: ":443"

    # Paths to TLS cert and key files.
    # If you change the cert & key files while chproxy is running, you have to restart chproxy so that it loads them.
    # Triggering a SIGHUP signal won't work as for the rest of the configuration.
    # cert_file: "cert_file"
    # key_file: "key_file"

    # Letsencrypt config.
    # Certificates are automatically issued and renewed if this section
    # is present.
    # There is no need in cert_file and key_file if this section is present.
    # Autocert requires application to listen on :80 port for certificate generation
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
    #
    # By default there is no per-minute limit.
    requests_per_minute: 4

    # Response cache config name to use.
    #
    # By default responses aren't cached.
    cache: "longterm"

    # An optional group of params to send to ClickHouse with each proxied request.
    # These params may be set in param_groups block.
    #
    # By default no additional params are sent to ClickHouse.
    params: "web"

    # The maximum number of requests that may wait for their chance
    # to be executed because they cannot run now due to the current limits.
    #
    # This option may be useful for handling request bursts from `tabix`
    # or `clickhouse-grafana`.
    #
    # By default all the requests are immediately executed without
    # waiting in the queue.
    max_queue_size: 100

    # The maximum duration the queued requests may wait for their chance
    # to be executed.
    # This option makes sense only if max_queue_size is set.
    # By default requests wait for up to 10 seconds in the queue.
    max_queue_time: 35s

  - name: "default"
    to_cluster: "second cluster"
    to_user: "default"
    allowed_networks: ["office", "1.2.3.0/24"]

    # The maximum number of concurrently running queries for the user.
    #
    # By default there is no limit on the number of concurrently
    # running queries.
    max_concurrent_queries: 4

    # The maximum query duration for the user.
    # Timed out queries are forcibly killed via `KILL QUERY`.
    #
    # By default set to 120s.
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

    # User configuration for heart beat requests.
    # Credentials of the first user in clusters.users will be used for heart beat requests to clickhouse.
    heartbeat:
      # An interval for checking all cluster nodes for availability
      # By default each node is checked for every 5 seconds.
      interval: 1m

      # A timeout of wait response from cluster nodes
      # By default 3s
      timeout: 10s

      # The parameter to set the URI to request in a health check
      # By default "/?query=SELECT%201"
      request: "/?query=SELECT%201%2B1"

      # Reference response from clickhouse on health check request
      # By default "1\n"
      response: "2\n"

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

    # The cluster may contain multiple replicas instead of flat nodes.
    #
    # Chproxy selects the least loaded node among the least loaded replicas.
    replicas:
      - name: "replica1"
        nodes: ["127.0.1.1:8443", "127.0.1.2:8443"]
      - name: "replica2"
        nodes: ["127.0.2.1:8443", "127.0.2.2:8443"]

    users:
      - name: "default"
        max_concurrent_queries: 4
        max_execution_time: 1m

      - name: "web"
        max_concurrent_queries: 4
        max_execution_time: 10s
        requests_per_minute: 10
        max_queue_size: 50
        max_queue_time: 70s
        allowed_networks: ["office"]
```

#### 完整的配置规范请参考[这里](https://github.com/ContentSquare/chproxy/blob/master/config)

## 指标
所有的指标都以 Prometheus 文本格式暴露在 `/metrics` 路径上。

| Name | Type | Description | Labels |
| ------------- | ------------- | ------------- | ------------- |
| bad_requests_total | Counter | The number of unsupported requests | |
| cache_hits_total | Counter | The amount of cache hits | `cache`, `user`, `cluster`, `cluster_user` |
| cache_items | Gauge | The number of items in each cache | `cache` |
| cache_miss_total | Counter | The amount of cache misses | `cache`, `user`, `cluster`, `cluster_user` |
| cache_size | Gauge | Size of each cache | `cache` |
| cached_response_duration_seconds | Summary | Duration for cached responses. Includes the duration for sending response to client | `cache`, `user`, `cluster`, `cluster_user` |
| canceled_request_total | Counter | The number of requests canceled by remote client | `user`, `cluster`, `cluster_user`, `replica`, `cluster_node` |
| cluster_user_queue_overflow_total | Counter | The number of overflows for per-cluster_user request queues | `user`, `cluster`, `cluster_user` |
| concurrent_limit_excess_total | Counter | The number of rejected requests due to max_concurrent_queries limit | `user`, `cluster`, `cluster_user`, `replica`, `cluster_node` |
| concurrent_queries | Gauge | The number of concurrent queries at the moment | `user`, `cluster`, `cluster_user`, `replica`, `cluster_node` |
| config_last_reload_successful | Gauge | Whether the last configuration reload attempt was successful | |
| config_last_reload_success_timestamp_seconds | Gauge | Timestamp of the last successful configuration reload | |
| host_health | Gauge | Health state of hosts by clusters | `cluster`, `replica`, `cluster_node` |
| host_penalties_total | Counter | The number of given penalties by host | `cluster`, `replica`, `cluster_node` |
| killed_request_total | Counter | The number of requests killed by proxy | `user`, `cluster`, `cluster_user`, `replica`, `cluster_node` |
| proxied_response_duration_seconds | Summary | Duration for responses proxied from ClickHouse | `user`, `cluster`, `cluster_user`, `replica`, `cluster_node` |
| request_body_bytes_total | Counter | The amount of bytes read from request bodies | `user`, `cluster`, `cluster_user`, `replica`, `cluster_node` |
| request_duration_seconds | Summary | Request duration. Includes possible queue wait time | `user`, `cluster`, `cluster_user`, `replica`, `cluster_node` |
| request_queue_size | Gauge | Request queue size at the moment | `user`, `cluster`, `cluster_user` |
| request_success_total | Counter | The number of successfully proxied requests | `user`, `cluster`, `cluster_user`, `replica`, `cluster_node` |
| request_sum_total | Counter | The number of processed requests | `user`, `cluster`, `cluster_user`, `replica`, `cluster_node` |
| response_body_bytes_total | Counter | The amount of bytes written to response bodies | `user`, `cluster`, `cluster_user`, `replica`, `cluster_node` |
| status_codes_total | Counter | Distribution by response status codes | `user`, `cluster`, `cluster_user`, `replica`, `cluster_node`, `code` |
| timeout_request_total | Counter | The number of timed out requests | `user`, `cluster`, `cluster_user`, `replica`, `cluster_node` |
| user_queue_overflow_total | Counter | The number of overflows for per-user request queues | `user`, `cluster`, `cluster_user` |

[这里](https://github.com/ContentSquare/chproxy/blob/master/chproxy_overview.json)与一个 [Grafana](https://grafana.com) 的 chproxy 指标仪表盘例子。

![dashboard example](https://user-images.githubusercontent.com/2902918/31392734-b2fd4a18-ade2-11e7-84a9-4aaaac4c10d7.png)


## FAQ

*  *`chproxy`  是否可用于生产环境？*

  是的，我们生产环境中将它成功用于 `INSERT` 和 `SELECT` 请求。

* *`chproxy` 性能表现怎么样？*

  在我们的生产环境中，单个 chproxy 实例可以在使用不到 20% 的单 CPU 核心资源情况下，轻松代理 1Gbps 的压缩 `INSERT` 数据。

*  *`chproxy`  是否支持 ClickHouse 的 Native 协议？*

  不支持，应为我们所有的所有的应用只通过 HTTP 协议与 ClickHouse 通讯。

  可能会在未来增加对 Native 协议的支持。
