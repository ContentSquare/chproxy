---
title: Metrics
sidebar:
    order: 5
---

Metrics are exposed in [Prometheus text format](https://prometheus.io/docs/instrumenting/exposition_formats/) at `/metrics` path.

| Name | Type | Description | Labels |
| ------------- | ------------- | ------------- | ------------- |
| retry_request_total | Counter | The number of retry requests | `user`, `cluster`, `cluster_user`, `replica`, `cluster_node` |
| bad_requests_total | Counter | The number of unsupported requests | |
| cache_hits_total | Counter | The amount of cache hits | `cache`, `user`, `cluster`, `cluster_user` |
| cache_items | Gauge | The number of items in each cache | `cache` |
| cache_miss_total | Counter | The amount of cache misses | `cache`, `user`, `cluster`, `cluster_user` |
| cache_size | Gauge | Size of each cache | `cache` |
| cached_response_duration_seconds | Summary | Duration for cached responses. Includes the duration for sending response to client | `cache`, `user`, `cluster`, `cluster_user` |
| canceled_request_total | Counter | The number of requests canceled by remote client | `user`, `cluster`, `cluster_user`, `replica`, `cluster_node` |
| cluster_user_queue_overflow_total | Counter | The number of overflows for per-cluster_user request queues | `user`, `cluster`, `cluster_user` |
| concurrent_limit_excess_total | Counter | The number of rejected requests due to max_concurrent_queries limit | `user`, `cluster`, `cluster_user`, `replica`, `cluster_node` |
| concurrent_queries | Gauge | The number of concurrent queries at the moment | `user`, `cluster`, `cluster_user`, `replica`, `cluster_node` |
| config_last_reload_successful | Gauge | Whether the last configuration reload attempt was successful | |
| config_last_reload_success_timestamp_seconds | Gauge | Timestamp of the last successful configuration reload | |
| host_health | Gauge | Health state of hosts by clusters | `cluster`, `replica`, `cluster_node` |
| host_penalties_total | Counter | The number of given penalties by host | `cluster`, `replica`, `cluster_node` |
| killed_request_total | Counter | The number of requests killed by proxy | `user`, `cluster`, `cluster_user`, `replica`, `cluster_node` |
| proxied_response_duration_seconds | Summary | Duration for responses proxied from ClickHouse | `user`, `cluster`, `cluster_user`, `replica`, `cluster_node` |
| request_body_bytes_total | Counter | The amount of bytes read from request bodies | `user`, `cluster`, `cluster_user`, `replica`, `cluster_node` |
| request_duration_seconds | Summary | Request duration. Includes possible queue wait time | `user`, `cluster`, `cluster_user`, `replica`, `cluster_node` |
| request_queue_size | Gauge | Request queue size at the moment | `user`, `cluster`, `cluster_user` |
| request_success_total | Counter | The number of successfully proxied requests | `user`, `cluster`, `cluster_user`, `replica`, `cluster_node` |
| request_sum_total | Counter | The number of processed requests | `user`, `cluster`, `cluster_user`, `replica`, `cluster_node` |
| response_body_bytes_total | Counter | The amount of bytes written to response bodies | `user`, `cluster`, `cluster_user`, `replica`, `cluster_node` |
| status_codes_total | Counter | Distribution by response status codes | `user`, `cluster`, `cluster_user`, `replica`, `cluster_node`, `code` |
| timeout_request_total | Counter | The number of timed out requests | `user`, `cluster`, `cluster_user`, `replica`, `cluster_node` |
| user_queue_overflow_total | Counter | The number of overflows for per-user request queues | `user`, `cluster`, `cluster_user` |

An example of [Grafana's](https://grafana.com) dashboard for `chproxy` metrics is available [here](https://github.com/ContentSquare/chproxy/blob/master/chproxy_overview.json).

![dashboard example](https://user-images.githubusercontent.com/2902918/31392734-b2fd4a18-ade2-11e7-84a9-4aaaac4c10d7.png)

