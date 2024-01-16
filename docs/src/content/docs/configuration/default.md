---
title: Default configuration
sidebar:
  label: Default
  order: 6
---


Example of [full](https://github.com/contentsquare/chproxy/blob/master/config/testdata/full.yml) configuration:

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

    # Cache mode, either [[file_system]] or [[redis]] 
    mode: "file_system"
    
    # Applicable for cache mode: file_system
    file_system:
      # Path to directory where cached responses will be stored.
      dir: "/path/to/longterm/cachedir"
    
      # Maximum cache size.
      # `Kb`, `Mb`, `Gb` and `Tb` suffixes may be used.
      max_size: 100Gb

    # Expiration time for cached responses.
    expire: 1h

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
    grace_time: 20s

  - name: "shortterm"
    mode: "redis"
    
    # Applicable for cache mode: redis
    # You should use multiple addresses only if they all belong to the same redis cluster.
    redis:
      # Paths to TLS cert and key files for the redis server.
      # If you change the cert & key files while chproxy is running, you have to restart chproxy so that it loads them.
      # Triggering a SIGHUP signal won't work as for the rest of the configuration.
      cert_file: "redis tls cert file path"
      key_file: "redis tls key file apth"
      # Allow to skip the verification of the redis server certificate.
      insecure_skip_verify: true

      addresses:
        - "localhost:16379"
      username: "user"
      password: "pass"
    expire: 10s

# Optional network lists, might be used as values for `allowed_networks`.
network_groups:
  - name: "office"
    # Each item may contain either IP or IP subnet mask.
    networks: ["127.0.0.0/24", "10.10.0.1"]

  - name: "reporting-apps"
    networks: ["10.10.10.0/24"]

# Maximum total size of fail reason of queries. Config prevents large tmp files from being read into memory, affects only cachable queries
# If error reason exceeds limit "unknown error reason" will be stored as a fail reason
max_error_reason_size: 100GB

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
    namespace: ""

  # Proxy settings enable parsing proxy headers in cases where
  # CHProxy is run behind another proxy.
  proxy:
    enable: true
    header: CF-Connecting-IP

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

#### Full specification is located [here](https://github.com/ContentSquare/chproxy/blob/master/config)


