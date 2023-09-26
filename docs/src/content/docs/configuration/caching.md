---
title: Caching
sidebar:
    order: 3
---

`Chproxy` may be configured to cache responses. It is possible to create multiple
cache-configs with various settings.

Response caching is enabled by assigning cache name to user. Multiple users may share the same cache.

Currently only `SELECT` responses are cached.

Caching is disabled for request with `no_cache=1` as an http query parameter. 
There's no support for similar feature within SQL query.


Optional cache namespace may be passed in query string as `cache_namespace=aaaa`. This allows caching
distinct responses for the identical query under distinct cache namespaces. Additionally,
an instant cache flush may be built on top of cache namespaces - just switch to new namespace in order
to flush the cache.

Two types of cache configuration are supported:
- local instance cache 
- distributed cache

#### Local cache
Local cache is stored on machine's file system. Therefore it is suitable for single replica deployments.
Configuration template for local cache can be found [here](https://github.com/ContentSquare/chproxy/blob/master/config/#file_system_cache_config).

#### Distributed cache
Distributed cache relies on external database to share cache across multiple replicas. Therefore it is suitable for 
multiple replicas deployments. Currently only [Redis](https://redis.io/) key value store is supported. 
Configuration template for distributed cache can be found [here](https://github.com/ContentSquare/chproxy/blob/master/config/#distributed_cache_config).

#### Response limitations for caching
Before caching Clickhouse response, chproxy verifies that the response size 
is not greater than configured max size. This setting can be specified in config section of the cache `max_payload_size`. The default value
is set to 1 Petabyte. Therefore, by default this security mechanism is disabled.

#### Thundering herd
When query arrives to the chproxy with activated cache, chproxy starts, so called, transaction. Its purpose is to prevent from thundering herd effect as such 
that the concurrent request relating to the exactly same query will await for the result of the computation from the first request.
Internally, chproxy hosts `transactions regsitry` which stores ongoing transactions, aka concurrent queries. They're identified by the hash of the query, the same way as caching behaves.  
There exists two types of `transaction registry` equivalent to cache alternatives:
- distributed transaction registry (redis based)
- local transaction registry (in RAM)

When Clickhouse responds to the firstly arrived query, existing key is updated accordingly:
- if succeeded, as completed
- if failed, as failed along with the exception message prepended with `[concurrent query failed]`.

Transaction is kept for the duration of 2 * grace_time or 2 * max_execution_time, depending if grace time is specified.

#### Cache shared with all users
Until version 1.19.0, the cache is shared with all users.
It means that if:
- a user X does a query A,
- then the result is cached,
- then a user Y does the same query A
User Y will get the cached response from user X's query.

Since 1.20.0, the cache is specific for each user by default since it's better in terms of security.
It's possible to use the previous behavior by setting the following property of the cache in the config file `shared_with_all_users = true` 

#### Detecting Cache Hits

`Chproxy` will respond with an `X-Cache` header with a value of `HIT` if it returned a response from either the local or the distributed cache. Otherwise `X-Cache` will be set to `MISS`. 
If the response couldn't be cached due to the configuration (e.g. a payload that is too large), `N/A` will be returned. This can be used for example to determine 
whether the ClickHouse query stats in the response can be trusted or are cached responses.
