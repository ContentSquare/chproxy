---
title: All the above configs combined
sidebar:
  label: All of the above
  order: 4
---


All the above cases may be combined in a single `chproxy` [config](https://github.com/ContentSquare/chproxy/blob/master/config/examples/combined.yml):

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
    mode: "file_system"
    file_system:
      dir: "/path/to/cache/dir"
      max_size: 150Mb
    expire: 130s
```

