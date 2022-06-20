---
title: Pass user names and passwords as is
menuTitle: Transparent users
category: Use Cases
position: 305
---

Suppose you need to use `ClickHouse` LDAP facilities. It is more secure and more flexible than having credentials hardcoded in ClickHouse or a `chproxy` configuration files.
The following `chproxy` config may be used for [this use case](https://github.com/ContentSquare/chproxy/blob/master/config/examples/transparent.yml):
```yml
log_debug: true

server:
  http:
      listen_addr: ":9090"
      allowed_networks: ["127.0.0.0/24"]

users:
  - name: "transparent_user"
    to_cluster: "default_with_transparent"
    to_user: "transparent_user"

# by default each cluster has `default` user which can be overridden by section `users`
clusters:
  - name: "default_with_transparent"
    nodes: ["127.0.0.1:8123"]

    users:
    - name: "transparent_user"
```

If `chproxy` cache is used together with `transparent_user` it is required to set `allow_transparent_cache` parameter to confirm awareness of security risks: different `ClickHouse` users share the same cache.
