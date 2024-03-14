### Possible types used in configuration:

 - `<bool>`: a boolean value `true` or `false`
 - `<addr>`: string value consisting of a hostname or IP followed by an optional port number
 - `<scheme>`: a string that can take the values `http` or `https`
 - `<duration>`: a duration matching the regular expression `^([0-9]+)(w|d|h|m|s|ms|µs|ns)`
 - `<networks>`: string value consisting of IP, IP mask or named group, for example `"127.0.0.1"` or `"127.0.0.1/24"`.
 - `<host_name>`: string value consisting of host name, for example `"example.com"`
 - `<byte_size>`: string value matching the regular expression `/^\d+(\.\d+)?[KMGTP]?B?$/i`, for example `"100MB"`

### Global configuration consist of:
```yml
# Whether to print debug logs
log_debug: <bool> | default = false [optional]

# Whether to ignore security warnings
hack_me_please: <bool> | default = false [optional]

# Named list of cache configurations
caches:
  - <cache_config> ...

# Named list of parameters to apply to each query
param_groups:
  - <param_groups_config> ...

# Named network lists
network_groups: <network_groups_config> ... [optional]

# Maximum total size of fail reason of queries. Config prevents large tmp files from being read into memory, affects only cachable queries
# The default value is set to 1 Petabyte.
# If error reason exceeds limit "unknown error reason" will be stored as a fail reason
max_error_reason_size: <byte_size>

# Settings for connection pool to ClickHouse
connection_pool:
  max_idle_conns: 100
  max_idle_conns_per_host: 2

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

### <file_system_cache_config>
```yml
# Cache name, which may be passed into `cache` option on the `user` level.
#
# Multiple users may share the same cache.
name: <string>

mode: "file_system"

file_system:
    # Path to directory where cached responses will be stored.
    dir: <string>

    # Maximum cache size.
    max_size: <byte_size>

# Expiration time for cached responses.
expire: <duration>

# DEPRECATED: default value equal to `max_execution_time` should be used.
#             New configuration parameter will be provided to disable the protection at will.
# When multiple requests with identical query simultaneously hit `chproxy`
# and there is no cached response for the query, then only a single
# request will be proxied to clickhouse. Other requests will wait
# for the cached response during this grace duration.
# This is known as protection from `thundering herd` problem.
#
# By default `grace_time` is 5s. Negative value disables the protection
# from `thundering herd` problem.
grace_time: <duration>

# Maximum total size of request payload for caching. The default value
# is set to 1 Petabyte.
# The default value set so high is to allow users who do not use response size limitations virtually unlimited cache.
max_payload_size: <byte_size>

# Whether a query cached by a user can be used by another user
shared_with_all_users: <bool> | default = false [optional]
```

### <distributed_cache_config>
```yml
# Cache name, which may be passed into `cache` option on the `user` level.
#
# Multiple users may share the same cache.
name: <string>

mode: "redis"

# Applicable for cache mode: redis
redis:
  # list of addresses to redis nodes
  # you should use multiple addresses only if they all belong to the same redis cluster.
  addresses:
    - <string> # example "localhost:6379"
  username: <string>
  password: <string>
  pool_size: <int>
  db_index: <int> | default = 0 [optional] # This option is only applicable for non-clustered Redis instance.

# Expiration time for cached responses.
expire: <duration>

# DEPRECATED: default value equal to `max_execution_time` should be used.
#             New configuration parameter will be provided to disable the protection at will.
# When multiple requests with identical query simultaneously hit `chproxy`
# and there is no cached response for the query, then only a single
# request will be proxied to clickhouse. Other requests will wait
# for the cached response during this grace duration.
# This is known as protection from `thundering herd` problem.
#
# By default `grace_time` is 5s. Negative value disables the protection
# from `thundering herd` problem.
grace_time: <duration>

# Maximum total size of request payload for caching. The default value
# is set to 1 Petabyte.
# The default value set so high is to allow users who do not use response size limitations virtually unlimited cache.
max_payload_size: <byte_size>

# Whether a query cached by a user can be used by another user
shared_with_all_users: <bool> | default = false [optional]
```

### <param_groups_config>
```yml
# Group name, which may be passed into `params` option on the `user` level.
- name: <string>
# List of key-value params to send
params:
  - key: <string>
    value: <string>
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

### <http_config>
```yml
# TCP address to listen to for http
listen_addr: <addr>

# List of networks or network_groups access is allowed from
# Each list item could be IP address or subnet mask
allowed_networks: <network_groups>, <networks> ... | optional

# ReadTimeout is the maximum duration for reading the entire
# request, including the body.
read_timeout: <duration> | optional | default = 1m

# WriteTimeout is the maximum duration before timing out writes of the response.
# Default is largest MaxExecutionTime + MaxQueueTime value from Users or Clusters
write_timeout: <duration> | optional

// IdleTimeout is the maximum amount of time to wait for the next request.
idle_timeout: <duration> | optional | default = 10m
```

### <https_config>
```yml
# TCP address to listen to for https
listen_addr: <addr> | optional | default = `:443`

# List of networks or network_groups access is allowed from
# Each list item could be IP address or subnet mask
allowed_networks: <network_groups>, <networks> ... | optional

# ReadTimeout is the maximum duration for proxy to reading the entire
# request, including the body.
read_timeout: <duration> | optional | default = 1m

# WriteTimeout is the maximum duration for proxy before timing out writes of the response.
# Default is largest MaxExecutionTime + MaxQueueTime value from Users or Clusters
write_timeout: <duration> | optional

// IdleTimeout is the maximum amount of time for proxy to wait for the next request.
idle_timeout: <duration> | optional | default = 10m

# Certificate and key files for client cert authentication to the server
# If you change the cert & key files while chproxy is running, you have to restart chproxy so that it loads them.
# Triggering a SIGHUP signal won't work as for the rest of the configuration.
cert_file: <string> | optional
key_file: <string> | optional

# Autocert configuration via letsencrypt
autocert: <autocert_config> | optional
```

### <autocert_config>
```yml
# Path to the directory where autocert certs are cached
cache_dir: <string>

# List of host names to which proxy is allowed to respond to
# see https://godoc.org/golang.org/x/crypto/acme/autocert#HostPolicy
allowed_hosts: <host_name> ... | optional
```

### <metrics_config>
```yml
# List of networks or network_groups access is allowed from
# Each list item could be IP address or subnet mask
allowed_networks: <network_groups>, <networks> ... | optional

# Prometheus metric namespace
namespace: <string> | optional
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

# Maximum number of concurrently running queries for user.
# By default there is no limit on the number of concurrently
# running queries.
max_concurrent_queries: <int> | optional | default = 0

# Maximum duration of query execution for user
# By default there is a 120 sec limit the query duration.
max_execution_time: <duration> | optional | default = 120s

# Maximum number of requests per minute for user.
# By default there are no per-minute limits
requests_per_minute: <int> | optional | default = 0

# The burst of request packet size token bucket for user
# By default there are no request packet size limits
request_packet_size_tokens_burst: <byte_size> | optional | default = 0

# The request packet size tokens produced rate per second for user
# By default there are no request packet size limits
request_packet_size_tokens_rate: <byte_size> | optional | default = 0

# Maximum number of requests waiting for execution in the queue.
# By default requests are executed without waiting in the queue
max_queue_size: <int> | optional | default = 0

# Maximum duration the request may wait in the queue.
# By default 10s duration is used
max_queue_time: <duration> | optional | default = 10s

# Whether to deny http connections for this user
deny_http: <bool> | optional | default = false

# Whether to deny https connections for this user
deny_https: <bool> | optional | default = false

# Whether to allow `CORS` requests for this user.
# Such requests are needed for `tabix`.
allow_cors: <bool> | optional | default = false

# List of networks or network_groups access is allowed from
# Each list item could be IP address or subnet mask
allowed_networks: <network_groups>, <networks> ... | optional

# Optional response cache name from <cache_config>
# By default responses aren't cached.
cache: <string> | optional

# Optional group of params name to send to ClickHouse with each proxied request from <param_groups_config>
# By default no additional params are sent to ClickHouse.
params: <string> | optional

# The user is wildcarded
# Name matches prefix* or *suffix or *
# Name and password to ClickHouse are obtained
# from original request, not from cluster user
is_wildcarded: <bool> | optional | default = false
```

### <cluster_config>
```yml
# Name of CH cluster, must match with `to_cluster`
name: <string>

# Scheme: `http` or `https`; would be applied to all nodes
scheme: <scheme> | optional | default = "http"

# Node addresses. Requests would be balanced among them.
#
# Either nodes or replicas may be configured, but not both.
nodes: <addr> ...

# The cluster may contain multiple replicas instead of flat nodes.
#
# Chproxy selects the least loaded node among the least loaded replicas.
#
# Either nodes or replicas may be configured, but not both.
replicas:
    - <replica_config>

# List of ClickHouse cluster users
users:
    - <cluster_user_config> ...

# KillQueryUser - user configuration for killing timed out queries.
# By default timed out queries are killed from `default` user.
kill_query_user: <kill_query_user_config> | optional

# HeartBeat - user configuration for heart beat requests.
heartbeat: <heartbeat_config> | optional

# RetryNumber - user configuration for query retry when one host cannot respond.
retry_number: 0

```

### <replica_config>
```yml
# Replica name
name: <string>

# Node addresses in the replica. Requests are balanced among them.
nodes: <addr> ...
```

### <cluster_user_config>
```yml
# User name in ClickHouse `users.xml` config
name: <string>

# User password in ClickHouse `users.xml` config
password: <string> | optional

# Maximum number of concurrently running queries for user
# By default there is no limit on the number of concurrently
# running queries.
max_concurrent_queries: <int> | optional | default = 0

# Maximum duration of query execution for user
# By default there is a 120 sec limit the query duration.
max_execution_time: <duration> | optional | default = 120s

# Maximum number of requests per minute for user.
# By default there are no per-minute limits
requests_per_minute: <int> | optional | default = 0

# The burst of request packet size token bucket for user
# By default there are no request packet size limits
request_packet_size_tokens_burst: <byte_size> | optional | default = 0

# The request packet size tokens produced rate per second for user
# By default there are no request packet size limits
request_packet_size_tokens_rate: <byte_size> | optional | default = 0

# Maximum number of requests waiting for execution in the queue.
# By default requests are executed without waiting in the queue
max_queue_size: <int> | optional | default = 0

# Maximum duration the request may wait in the queue.
# By default 10s duration is used
max_queue_time: <duration> | optional | default = 10s
```

### <kill_query_user_config>
```yml
# User name to access CH with basic auth
name: <string>

# User password to access CH with basic auth
password: <string> | optional
```

### <heartbeat_config>
```yml
# An interval for checking all cluster nodes for availability
interval: <duration> | optional | default = 5s

# A timeout of wait response from cluster nodes
timeout: <duration> | optional | default = 3s

# The parameter to set the URI to request in a health check
request: <string> | optional | default = `/?query=SELECT%201`

# Reference response from clickhouse on health check request
response: <string> | optional | default = `1\n`

# Credentials to send heartbeat requests
# for anything except '/ping'.
# If not specified, the first cluster user' creadentials are used
user: <string> | optional
password: <string> | optional
```
