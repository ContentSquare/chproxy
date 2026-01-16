# Chproxy Caching TTL (Time To Live)

## Overview

Chproxy supports caching query responses to improve performance and reduce load on ClickHouse clusters. The Time To Live (TTL) determines how long cached responses remain valid before they expire and need to be refreshed.

## TTL Configuration

The caching TTL is configured using the `expire` parameter in the cache configuration section. This parameter accepts a duration value in the format: `<number><unit>` where unit can be:
- `ns` - nanoseconds
- `µs` or `us` - microseconds
- `ms` - milliseconds
- `s` - seconds
- `m` - minutes
- `h` - hours
- `d` - days
- `w` - weeks

### Required Configuration

The `expire` parameter is **required** for all cache configurations. If not specified, the configuration will be invalid.

## Cache Modes

Chproxy supports two cache modes, both using the same `expire` configuration:

### 1. File System Cache

```yaml
caches:
  - name: "my_cache"
    mode: "file_system"
    file_system:
      dir: "/path/to/cache/dir"
      max_size: 150Mb
    expire: 1h           # Cached responses expire after 1 hour
```

### 2. Redis Cache

```yaml
caches:
  - name: "redis_cache"
    mode: "redis"
    redis:
      addresses:
        - "localhost:6379"
      username: "user"
      password: "password"
    expire: 30s          # Cached responses expire after 30 seconds
```

## Common TTL Examples

### Short-term Caching (Fast-changing data)
```yaml
expire: 10s              # 10 seconds
```

### Medium-term Caching (Moderate refresh rate)
```yaml
expire: 5m               # 5 minutes
expire: 30m              # 30 minutes
```

### Long-term Caching (Slowly-changing data)
```yaml
expire: 1h               # 1 hour
expire: 24h              # 24 hours (1 day)
```

## How TTL Works

1. **When a query is cached**: The current timestamp is recorded along with the cached response.

2. **When retrieving from cache**: 
   - The age of the cached entry is calculated
   - If `age <= expire`: The cached response is served immediately
   - If `age > expire`: The cached entry is considered expired

3. **Grace Time (deprecated)**: 
   - An additional `grace_time` parameter exists but is deprecated
   - During grace time, expired entries can still be served while being refreshed in the background
   - Default grace time is 5 seconds if not specified
   - In future versions, this will be replaced by `max_execution_time`

## TTL in HTTP Response Headers

When serving cached responses, chproxy includes caching information in the HTTP response headers:

```http
Cache-Control: max-age=<remaining_seconds>
```

Where `<remaining_seconds>` is the number of seconds until the cached entry expires (calculated as `expire - age`).

## Implementation Details

### File System Cache
- Location: `cache/filesystem_cache.go`
- Expired files are removed during periodic cleanup operations
- Cleanup runs at intervals of `expire/2` (minimum 1 minute)
- Files older than `expire + grace_time` are permanently deleted

### Redis Cache
- Location: `cache/redis_cache.go`
- Redis native TTL mechanism is used
- Entries automatically expire in Redis after the configured TTL
- For large payloads with low TTL (<15s), temporary files may be used to prevent data loss during streaming
  - This threshold is defined as `minTTLForRedisStreamingReader = 15 * time.Second` in the source code
  - Below this threshold, data is cached in temporary files to ensure complete delivery even if the Redis entry expires during transfer

## Transaction Registry TTL

In addition to cache entry TTL, chproxy uses transaction registries to prevent "thundering herd" problems (multiple identical queries hitting the backend simultaneously):

- **Transaction deadline**: Set to `expire + grace_time`
- **Transaction ended TTL**: 500 milliseconds (hardcoded in `cache/transaction_registry.go`)

## Best Practices

1. **Match TTL to data freshness requirements**: 
   - Real-time dashboards: 10s - 30s
   - Analytics reports: 5m - 1h
   - Historical data: 1h - 24h

2. **Consider query execution time**: 
   - TTL should be significantly longer than query execution time
   - Otherwise, cached entries may expire before being useful

3. **Balance between freshness and performance**: 
   - Shorter TTL = fresher data but more backend load
   - Longer TTL = better performance but potentially stale data

4. **Use different caches for different use cases**:
   ```yaml
   caches:
     - name: "realtime"
       expire: 30s
     - name: "reporting"
       expire: 1h
   ```

## Validation

To verify your TTL configuration is valid:

```bash
# chproxy will validate the configuration on startup
./chproxy -config=config.yml
```

If `expire` is missing or zero, chproxy will fail when trying to initialize the cache with an error message like:
```
FATAL: error while applying config: `expire` must be positive
```

Note: The `expire` field is technically optional in the YAML syntax (it has `omitempty` tag), but validation occurs when cache instances are created, ensuring it must be set to a positive value for the cache to function.

## Related Configuration Files

- `config/config.go` - Main configuration structure and validation
- `config/README.md` - Complete configuration reference
- `cache/filesystem_cache.go` - File system cache implementation
- `cache/redis_cache.go` - Redis cache implementation

## See Also

- [Configuration Documentation](config/README.md)
- [Cache Configuration Examples](config/examples/)
- [Official Documentation](https://www.chproxy.org/)
