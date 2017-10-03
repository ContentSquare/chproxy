### Global configuration consist of:
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