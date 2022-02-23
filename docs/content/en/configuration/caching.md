---
title: Caching
category: Configuration
position: 203
---

`Chproxy` may be configured to cache responses. It is possible to create multiple
cache-configs with various settings.

Response caching is enabled by assigning cache name to user. Multiple users may share the same cache.

Currently only `SELECT` responses are cached.

Caching is disabled for request with `no_cache=1` in query string.

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


