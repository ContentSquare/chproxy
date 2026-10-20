---
title: Traffic Cloning
sidebar:
    order: 8
---

Traffic cloning allows `chproxy` to mirror accepted requests to one or more additional ClickHouse clusters.
It is intended for keeping a pre-production or validation environment close to production without changing the
response returned to the client.

The feature is optional. If the `traffic_cloning` section is absent, `chproxy` behaves exactly as before and no
traffic is mirrored.

## Basic configuration

Add a top-level `traffic_cloning` section:

```yml
traffic_cloning:
  temp_dir: /var/lib/chproxy/traffic-clone
  max_concurrent_captures: 4
  max_capture_size: 1GB
  targets:
    - name: preprod_raw
      from_cluster: production_raw
      to_cluster: preprod_raw
      to_user: default
      percent: 10
      timeout: 30s
```

The target cluster must already exist in the regular `clusters` section. The target user must exist in the target
cluster's `users` list.

```yml
clusters:
  - name: production_raw
    nodes: ["prod-clickhouse-01:8123"]
    users:
      - name: default

  - name: preprod_raw
    nodes: ["preprod-clickhouse-01:8123"]
    users:
      - name: default
```

## Enabling and disabling

Traffic cloning is enabled when `traffic_cloning.targets` contains at least one target:

```yml
traffic_cloning:
  targets:
    - name: preprod
      to_cluster: preprod
      to_user: default
```

It may also be enabled explicitly:

```yml
traffic_cloning:
  enable: true
  targets:
    - name: preprod
      to_cluster: preprod
      to_user: default
```

To disable traffic cloning, remove the `traffic_cloning` section or set an empty configuration with no targets:

```yml
# traffic_cloning section omitted
```

Setting `enable: true` without targets is invalid and configuration loading will fail.

## Source filtering

By default, a clone target receives sampled traffic from all source clusters. Use `from_cluster` to clone only
traffic that was routed to a specific source cluster:

```yml
traffic_cloning:
  targets:
    - name: preprod_raw
      from_cluster: production_raw
      to_cluster: preprod_raw
      to_user: default
```

Additional optional filters may be used for narrower routing:

```yml
traffic_cloning:
  targets:
    - name: preprod_api_raw
      from_cluster: production_raw
      from_user: api
      from_cluster_user: raw_writer
      to_cluster: preprod_raw
      to_user: raw_writer
```

All configured source filters must match for a request to be cloned.

## Multiple targets

The same source traffic can be cloned to multiple clusters:

```yml
traffic_cloning:
  targets:
    - name: preprod_raw
      from_cluster: production_raw
      to_cluster: preprod_raw
      to_user: default
      percent: 25

    - name: qa_raw
      from_cluster: production_raw
      to_cluster: qa_raw
      to_user: default
      percent: 5
```

Each target makes an independent sampling decision and has its own metrics.

## Sampling percentage

`percent` controls how much matching traffic is cloned to a target:

```yml
percent: 10
```

Valid values are from `0` to `100`. If omitted, the default is `100`.

## Request execution model

Clone requests are sent asynchronously. The client response is always produced from the primary ClickHouse request,
not from the cloned request.

If the clone target fails, times out, or returns a non-200 status code, the primary request continues normally. Clone
failures are reported only through logs and metrics.

For `POST` requests, the request body is captured to a temporary file while the primary request is being read. Clone
requests are sent only after the body has been fully captured. This prevents partial mirrored `INSERT` requests and
avoids buffering the whole request body in memory.

The temporary file is removed after all selected clone targets finish. The primary request may still be affected by
local temporary-file I/O, but it does not wait for the mirror ClickHouse cluster to read the request body.

By default, captures are created in the operating system temporary directory. Use `temp_dir` to place them on a
dedicated filesystem:

```yml
traffic_cloning:
  temp_dir: /var/lib/chproxy/traffic-clone
```

The directory is created automatically when the configuration is applied. It must be writable by the `chproxy`
process. If it cannot be created or written to, configuration loading fails.

On startup, `chproxy` removes any leftover temporary capture files from the configured `temp_dir`. This ensures
that a previous crash or unclean shutdown does not leave stale files behind.

Use `max_concurrent_captures` to cap how many request bodies may be captured and replayed to clone targets at the
same time:

```yml
traffic_cloning:
  max_concurrent_captures: 4
```

If the limit is reached, matching requests are not cloned and the primary request continues normally. The skipped
clone is reported as `traffic_clone_requests_total{result="skipped"}`.

Use `max_concurrent_clones` to cap how many clone send goroutines may run at the same time across all targets:

```yml
traffic_cloning:
  max_concurrent_clones: 10
```

If the limit is reached when a request is about to be mirrored, that clone is skipped for the affected target and
recorded as `traffic_clone_requests_total{result="skipped"}`. The primary request is not affected. If omitted or
zero, there is no limit.

Use `max_capture_size` to avoid mirroring unexpectedly large request bodies:

```yml
traffic_cloning:
  max_capture_size: 1GB
```

If the request body is larger than this value, it is not cloned and the primary request continues normally. If omitted
or zero, no size limit is applied.

`max_queued_chunks` is accepted for backwards compatibility with the previous streaming implementation, but it is not
used by the temporary-file capture path.

## Timeouts

`timeout` limits the execution time of a cloned request:

```yml
timeout: 30s
```

If omitted or set to zero, no additional clone-specific timeout is applied.

## Logging

Traffic cloning uses the existing `chproxy` logger.

Debug logs are controlled by the top-level `log_debug` option:

```yml
log_debug: true
```

When debug logging is enabled, clone request completion, failures, and body drops are logged. When `log_debug` is
disabled, the module stays quiet unless regular error logging is triggered elsewhere.

`log_masks` are still applied by the global logger, so sensitive values can be masked in the same way as for the rest
of `chproxy`.

## Metrics

Traffic cloning exposes Prometheus metrics with the same namespace and constant labels configured in `server.metrics`.

| Name | Type | Description |
| --- | --- | --- |
| `traffic_clone_requests_total` | Counter | Clone requests by result: `success`, `error`, `canceled`, or `skipped` |
| `traffic_clone_status_codes_total` | Counter | Status codes returned by clone targets |
| `traffic_clone_duration_seconds` | Summary | Clone request duration |
| `traffic_clone_in_flight` | Gauge | Clone requests currently in flight |

Common labels:

- `user`
- `cluster`
- `cluster_user`
- `target`
- `target_cluster`
- `target_cluster_user`

`traffic_clone_requests_total` also has a `result` label. `traffic_clone_status_codes_total` also has a `code` label.
