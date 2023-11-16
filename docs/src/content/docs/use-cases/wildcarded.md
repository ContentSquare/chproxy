---
title: Pass user names and passwords as is
sidebar:
  label: Wildcarded users
  order: 5
---

Suppose you need to use `ClickHouse` LDAP facilities. It is more secure and more flexible than having credentials hardcoded in ClickHouse or a `chproxy` configuration files.
The following `chproxy` config may be used for [this use case](https://github.com/ContentSquare/chproxy/blob/master/config/examples/wildcarded.yml):
```yml
log_debug: true

users:
  # wildcarded user
  # matches with any name with prefix 'analyst_'
  # e.g. 'analyst_joe' or 'analyst_jane'
  - name: "analyst_*"
    to_cluster: "default"
    to_user: "analyst_*"
    is_wildcarded: true
  - name: "dba"
    password: "dba_ingress_pass"
    to_cluster: "default"
    to_user: "dba"
clusters:
  - name: "default"
    nodes: ["127.0.0.1:8123"]

    users:
    - name: "analyst_*"
    - name: "dba"
      password: "dba_egress_pass"
```

Wildcarded user has "_*" suffix.
Original name and original password are used in requests to ClickHouse
