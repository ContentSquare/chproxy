# CHProxy [![Go Report Card](https://goreportcard.com/badge/github.com/Vertamedia/chproxy)](https://goreportcard.com/report/github.com/Vertamedia/chproxy)
[![Build Status](https://travis-ci.org/Vertamedia/chproxy.svg?branch=master)](https://travis-ci.org/Vertamedia/chproxy.svg?branch=master)


CHProxy, is a web-proxy for accessing [ClickHouse](https://clickhouse.yandex) database. It supports next features:

- https mode with custom cert or using autocert
- limit access by IP or IP network
- limit access by user/password
- max_concurrent_queries and max_execution_time limits
- multiple cluster configuration
- metrics via prometheus library


## Configuration

Example of full configuration can be found [here](https://github.com/Vertamedia/chproxy/blob/master/config/testdata/full.yml)


### Possible types used in configuration:

 - <bool>: a boolean value `true` or `false`
 - <networks>: string value consisting of IP or IP mask, for example `"127.0.0.1"` or `"127.0.0.1/24"`
 - <host_name>: string value consisting of host name, for example `"example.com"`
 - <addr>: string value consisting of a hostname or IP followed by an optional port number
 - <scheme>: a string that can take the values `http` or `https`
 - <duration>: a duration matching the regular expression `[0-9]+(ms|[smhdwy])`


Global configuration consist of:
```yml
// Whether to print debug logs
[ log_debug: <bool> | default = false ]

// List of networks that access is allowed from
// Each list item could be IP address or subnet mask
// if omitted or zero - no limits would be applied
[ allowed_networks: <networks> ... ]

server:
  [ [`<server_config>`](#server_config) ]

// List of allowed users
// which requests will be proxied to ClickHouse
users:
  - <user_config> ...

clusters:
  - <cluster_config> ...
```

### <server_config>:
```
// TCP address to listen to for http
[ listen_addr: <addr> | default = `localhost:8080` ]

// Whether serve https at `listen_addr` addr
// If no `tls_config` specified than `autocert` will be used
[ is_tls: <bool> ]

// TLS configuration
tls_config:
  [ <tls_config> ]
```

### <tls_config>:
```
// Path to the directory where autocert certs are cached
[ cert_cache_dir: <string> ]

// List of host names to which proxy is allowed to respond to
// see https://godoc.org/golang.org/x/crypto/acme/autocert#HostPolicy
[ host_policy: <host_name> ... ]

# Certificate and key files for client cert authentication to the server
[ cert_file: <string> ]
[ key_file: <string> ]
```

### <user_config>:
```
# User name, will be taken from BasicAuth or from URL `user`-param
name: <string>

# User password, will be taken from BasicAuth or from URL `password`-param
[ password: <string> ]

// Must match with name of `cluster` config,
// where requests will be proxied
to_cluster: <string>

// Must match with name of `user` from `cluster` config,
// whom credentials will be used for proxying request to CH
to_user: <string>

# Maximum number of concurrently running queries for user
[ max_concurrent_queries: <int> | default = 0 ]

# Maximum duration of query execution for user
[ max_execution_time: <duration> | default = 0 ]

// List of networks that access is allowed from
// Each list item could be IP address or subnet mask
// if omitted or zero - no limits would be applied
[ allowed_networks: <networks> ... ]

```

### <cluster_config>:
```
# Name of CH cluster, must match with `to_cluster`
name: <string>

# Scheme: `http` or `https`; would be applied to all nodes
[ scheme: <scheme> | default = "http" ]

# List of nodes addresses. Requests would be balanced among them
nodes: <addr> ...

# List of ClickHouse cluster users
users:
    - <cluster_user_config> ...

# KillQueryUser - user configuration for killing
# queries which has exceeded limits
# if not specified - killing queries will be omitted
[ kill_query_user: <kill_query_user_config> ]

# An interval of checking all cluster nodes for availability
[ heartbeat_interval: <duration> | default = 5s ]
```

### <cluster_user_config>:
```
# User name in ClickHouse `users.xml` config
name: <string>

# User password in ClickHouse `users.xml` config
[ password: <string> ]

# Maximum number of concurrently running queries for user
[ max_concurrent_queries: <int> | default = 0 ]

# Maximum duration of query execution for user
[ max_execution_time: <duration> | default = 0 ]
```

### <kill_query_user_config>:
```
# User name to access CH with basic auth
name: <string>

# User password to access CH with basic auth
[ password: <string> ]
```