# Chproxy Caching TTL - Quick Reference

## Summary

Chproxy's caching TTL (Time To Live) determines how long query responses are cached before expiring. This document provides a quick reference to the caching TTL configuration.

## Key Information

### Where TTL is Configured

The caching TTL is configured in the `caches` section of the configuration file using the `expire` parameter:

```yaml
caches:
  - name: "my_cache"
    mode: "file_system"  # or "redis"
    expire: 1h           # TTL value - REQUIRED
    # ... other settings
```

### Default TTL

**There is NO default TTL value** - the `expire` parameter is **mandatory** and must be explicitly specified. If omitted, chproxy will fail to start with an error.

### Duration Format

TTL values use Go's duration format: `<number><unit>`

| Unit | Description | Example |
|------|-------------|---------|
| `ns` | Nanoseconds | `1000ns` |
| `µs` or `us` | Microseconds | `100µs` |
| `ms` | Milliseconds | `500ms` |
| `s` | Seconds | `30s` |
| `m` | Minutes | `5m` |
| `h` | Hours | `2h` |
| `d` | Days | `7d` |
| `w` | Weeks | `2w` |

### Common TTL Values

| Use Case | Recommended TTL | Example |
|----------|----------------|---------|
| Real-time dashboards | 5s - 30s | `expire: 10s` |
| Live analytics | 1m - 5m | `expire: 5m` |
| Hourly reports | 30m - 1h | `expire: 1h` |
| Daily reports | 6h - 24h | `expire: 24h` |
| Historical/static data | 24h - 7d | `expire: 168h` |

## Quick Start

### Minimal Configuration

```yaml
caches:
  - name: "default_cache"
    mode: "file_system"
    file_system:
      dir: "/tmp/chproxy-cache"
      max_size: 1Gb
    expire: 30s           # Cache for 30 seconds
```

### Multiple Caches with Different TTLs

```yaml
caches:
  - name: "fast"
    mode: "file_system"
    file_system:
      dir: "/tmp/cache/fast"
      max_size: 1Gb
    expire: 10s           # Short TTL for frequently changing data
    
  - name: "slow"
    mode: "file_system"
    file_system:
      dir: "/tmp/cache/slow"
      max_size: 10Gb
    expire: 1h            # Long TTL for stable data
```

## How It Works

1. **First Request**: Query is executed on ClickHouse, result is cached with timestamp
2. **Subsequent Requests**: 
   - If `age < expire`: Cached result is served immediately
   - If `age >= expire`: Cache entry is expired, query is re-executed
3. **HTTP Header**: Cached responses include `Cache-Control: max-age=<remaining_ttl_seconds>`

## Code Locations

| Component | File |
|-----------|------|
| Configuration structure | `config/config.go` (line 928) |
| File system cache | `cache/filesystem_cache.go` |
| Redis cache | `cache/redis_cache.go` |
| Cache interface | `cache/cache.go` |

## Documentation

- **Detailed Guide**: [CACHING_TTL.md](./CACHING_TTL.md)
- **Configuration Reference**: [config/README.md](./config/README.md)
- **Example Configurations**: 
  - [config/examples/cache_ttl_examples.yml](./config/examples/cache_ttl_examples.yml)
  - [config/examples/simple_cache_ttl_test.yml](./config/examples/simple_cache_ttl_test.yml)

## Validation

To validate your TTL configuration:

```bash
./chproxy -config=your_config.yml
```

If the configuration is valid, chproxy will start (or fail with a different error if services are unavailable).

If `expire` is missing or invalid, you'll see an error like:
```
FATAL: error while applying config: `expire` must be positive
```

## Testing

Use the provided example configuration to test TTL settings:

```bash
./chproxy -config=config/examples/simple_cache_ttl_test.yml
```

## Additional Notes

- **Grace Time**: A deprecated `grace_time` parameter exists that extends the cache lifetime slightly. This will be removed in future versions.
- **Redis TTL**: When using Redis cache mode, the native Redis TTL mechanism is used.
- **Transaction Registry**: Internal transaction tracking uses a fixed 500ms TTL after completion.
- **HTTP Streaming**: For Redis cache with TTL < 15s and large payloads, temporary files may be used to prevent data loss.

## Need Help?

- Read the [full documentation](./CACHING_TTL.md)
- Check [configuration examples](./config/examples/)
- Visit [chproxy.org](https://www.chproxy.org/)
