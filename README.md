[![Go Report Card](https://goreportcard.com/badge/github.com/ContentSquare/chproxy)](https://goreportcard.com/report/github.com/ContentSquare/chproxy)
[![Go Coverage](https://github.com/ContentSquare/chproxy/wiki/coverage.svg)](https://raw.githack.com/wiki/ContentSquare/chproxy/coverage.html)
# chproxy

Chproxy is an HTTP proxy and load balancer for the [ClickHouse](https://ClickHouse.yandex) database.

It is an open-source community project and not an official ClickHouse project.

Full documentation is available on [the official website](https://www.chproxy.org/).

## Key Features

- **Query Caching**: Cache query responses with configurable TTL (Time To Live)
  - See [CACHING_TTL.md](./CACHING_TTL.md) for detailed information about caching TTL configuration
- **Load Balancing**: Distribute queries across multiple ClickHouse nodes
- **Security**: User authentication, network restrictions, and HTTPS support
- **Rate Limiting**: Control query execution and request rates per user
- **High Availability**: Automatic failover and health checks

## Contributing

See our [contributing guide](./CONTRIBUTING.md)
