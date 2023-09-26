---
title: Authorize users by passwords via HTTPS
sidebar:
  label: Authorizing users
  order: 3
---

Suppose you need to access `ClickHouse` cluster from anywhere by username/password.
This may be used for building graphs from [ClickHouse-grafana](https://github.com/Altinity/clickhouse-grafana) or [Tabix](https://tabix.io/).
It is bad idea to transfer unencrypted password and data over untrusted networks.
So HTTPS must be used for accessing the cluster in such cases.
The following `chproxy` config may be used for [this use case](https://github.com/ContentSquare/chproxy/blob/master/config/examples/https.yml):
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
    mode: "file_system"
    file_system:
      dir: "/path/to/cache/dir"
      max_size: 150Mb

    # Cached responses will expire in 130s.
    expire: 130s
```

