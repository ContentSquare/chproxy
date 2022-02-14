---
title: Security
category: Configuration
position: 204
---

`Chproxy` removes all the query params from input requests (except the user's [params](https://github.com/ContentSquare/chproxy/blob/master/config#param_groups_config) and listed [here](https://github.com/ContentSquare/chproxy/blob/master/scope.go#L292))
before proxying them to `ClickHouse` nodes. This prevents from unsafe overriding
of various `ClickHouse` [settings](http://clickhouse-docs.readthedocs.io/en/latest/interfaces/http_interface.html).

Be careful when configuring limits, allowed networks, passwords etc.
By default `chproxy` tries detecting the most obvious configuration errors such as `allowed_networks: ["0.0.0.0/0"]` or sending passwords via unencrypted HTTP.

Special option `hack_me_please: true` may be used for disabling all the security-related checks during config validation (if you are feeling lucky :) ).

