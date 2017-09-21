# CHProxy [![Go Report Card](https://goreportcard.com/badge/github.com/Vertamedia/chproxy)](https://goreportcard.com/report/github.com/Vertamedia/chproxy)
[![Build Status](https://travis-ci.org/Vertamedia/chproxy.svg?branch=master)](https://travis-ci.org/Vertamedia/chproxy.svg?branch=master)


CHProxy, is a web-proxy for accessing [ClickHouse](https://clickhouse.yandex) database. It supports next features:

- https mode with custom cert or using autocert
- limit access by IP or IP mask
- limit access by user/password
- `max_concurrent_queries` and` max_execution_time` limits
- multiple cluster configuration
- expose metrics via prometheus library

## Why it was created
We faced a situation when ClickHouse run out of user limits `max_execution_time` and `max_concurrent_queries` while calculating queries.
Which leads to high resource usage on all cluster nodes and performance worsening. The only way out was to kill those queries manually (strange, but ClickHouse didn't kill them by itself)
or rebooting. So considering a fact that we already were using a proxy to balance load between nodes, we decided to extend it with similar limits
and additional functionality.

## How it works

### Server
CHProxy allows to setup web-proxy with `HTTP` or `HTTPS` protocols. (`HTTPS`)[#<tls_config>] might be configured with
custom certificate or with automated (Let's Encrypt)[https://letsencrypt.org/] certificates. Property `listen_addr` is also used for `HTTPS`.

Access to proxy can be (limitied)[#networks] by list of IPs or IP masks. This option can be applied to
global proxy configuration or to (`user`)[#<user_config>].

### Users
There are two types of users: `in-users` (in global section) and `out-users` (in cluster section).
This means all requests to proxy will be matched to in-users at first and if all checks are ok - will be matched to out-users
with overriding credentials.


For example, we have one ClickHouse user `web` with `read-only` permissions and limits `max_concurrent_queries`=4.
And two applications which are `reading` from ClickHouse. So we are creating two `in-users` with `max_concurrent_queries`=2 and `to_user`=web.
This will help avoid situation when one application will use all 4-request limit.


All requests to CHProxy must be authorized with credentials from (user-list)[#<user_config>]. Credentials can be passed
via BasicAuth or via URL `user` and `password` params. Limits for `in-users` and `out-users` are independent.

### Clusters
Proxy can be configured with multiple clusters. Each `cluster` must have a name and list of nodes.
All requests to cluster will be balanced with "round-robin least-loaded" way. It means that proxy will chose next least loaded node
for every new request.


There is also `heartbeat_interval` which is just checking all nodes for availability. If node is unavailable it will be excluded
from the list until connection will be restored. Such behavior must help to reduce number of unsuccessful requests in case of network lags.


If some of proxied queries through cluster will run out of `max_execution_time` limit, proxy will try to kill them.
But this is possible only if `cluster` configured with (`kill_query_user`)[#<kill_query_user_config>]


If `cluster`'s (`users`)[#cluster_user_config] are not specified, it means that there is only a "default" user with no limits.


## Configuration

Example of full configuration can be found [here](https://github.com/Vertamedia/chproxy/blob/master/config/testdata/full.yml) or simplest [here](https://github.com/Vertamedia/chproxy/blob/master/config/testdata/default_values.yml)

### Possible types used in configuration:

 - `<bool>`: a boolean value `true` or `false`
 - `<addr>`: string value consisting of a hostname or IP followed by an optional port number
 - `<scheme>`: a string that can take the values `http` or `https`
 - `<duration>`: a duration matching the regular expression `[0-9]+(ms|[smhdwy])`
 - (`<networks>`)[#networks]: string value consisting of IP or IP mask, for example `"127.0.0.1"` or `"127.0.0.1/24"`
 - `<host_name>`: string value consisting of host name, for example `"example.com"`


Global configuration consist of:
```yml
# Whether to print debug logs
log_debug: <bool> | default = false [optional]

# List of networks that access is allowed from
# Each list item could be IP address or subnet mask
# if omitted or zero - no limits would be applied
# Optional
allowed_networks: <networks> ... [optional]

server:
  <server_config> [optional]

# List of allowed users
# which requests will be proxied to ClickHouse
users:
  - <user_config> ...

clusters:
  - <cluster_config> ...
```

### <server_config>
```yml
# TCP address to listen to for http
listen_addr: <addr> | default = `localhost:8080` [optional]

# Whether serve https at `listen_addr` addr
# If no `tls_config` specified than `autocert` will be used
is_tls: <bool> | default = false [optional]

# TLS configuration
tls_config:
  <tls_config> [optional]
```

### <tls_config>
```yml
# Path to the directory where autocert certs are cached
cert_cache_dir: <string> [optional]

# List of host names to which proxy is allowed to respond to
# see https://godoc.org/golang.org/x/crypto/acme/autocert#HostPolicy
[ host_policy: <host_name> ... ]

# Certificate and key files for client cert authentication to the server
cert_file: <string> [optional]
key_file: <string> [optional]
```

### <user_config>
```yml
# User name, will be taken from BasicAuth or from URL `user`-param
name: <string>

# User password, will be taken from BasicAuth or from URL `password`-param
password: <string> [optional]

# Must match with name of `cluster` config,
# where requests will be proxied
to_cluster: <string>

# Must match with name of `user` from `cluster` config,
# whom credentials will be used for proxying request to CH
to_user: <string>

# Maximum number of concurrently running queries for user
max_concurrent_queries: <int> | default = 0 [optional]

# Maximum duration of query execution for user
max_execution_time: <duration> | default = 0 [optional]

# List of networks that access is allowed from
# Each list item could be IP address or subnet mask
# if omitted or zero - no limits would be applied
allowed_networks: <networks> ... [optional]

```

### <cluster_config>
```yml
# Name of CH cluster, must match with `to_cluster`
name: <string>

# Scheme: `http` or `https`; would be applied to all nodes
scheme: <scheme> | default = "http" [optional]

# List of nodes addresses. Requests would be balanced among them
nodes: <addr> ...

# List of ClickHouse cluster users
users:
    - <cluster_user_config> ...

# KillQueryUser - user configuration for killing
# queries which has exceeded limits
# if not specified - killing queries will be omitted
kill_query_user: <kill_query_user_config> [optional]

# An interval of checking all cluster nodes for availability
heartbeat_interval: <duration> | default = 5s [optional]
```

### <cluster_user_config>
```yml
# User name in ClickHouse `users.xml` config
name: <string>

# User password in ClickHouse `users.xml` config
password: <string> [optional]

# Maximum number of concurrently running queries for user
max_concurrent_queries: <int> | default = 0 [optional]

# Maximum duration of query execution for user
max_execution_time: <duration> | default = 0 [optional]
```

### <kill_query_user_config>
```yml
# User name to access CH with basic auth
name: <string>

# User password to access CH with basic auth
password: <string> [optional]
```

## Metrics
tbd

