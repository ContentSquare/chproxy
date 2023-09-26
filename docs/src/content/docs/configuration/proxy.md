---
title: Network Proxy
sidebar:
    order: 7
---

By default, `Chproxy` extracts the HTTP requests remote address. `Chproxy` can be configured to run behind another network proxy as well, by including the following configuration:

```yml
server:
  proxy:
    enable: true
```

When `Chproxy` is run with proxy mode enabled, `Chproxy` will try to parse the following headers to extract the remote address:

- `X-Forwarded-For`
- `X-Real-IP`
- `Forwarded`

If multiple remote address are found `Chproxy` assumes the first IP address is the actual remote address. For example in the case where `X-Forwarded-For: 10.0.0.1, 10.3.2.1`, `Chproxy` assumes `10.0.0.1` is the correct address.

If you have a custom header that contains the real remote address, it is possible to configure `Chproxy` to parse that header instead of the common proxy headers:

```yml
server:
  proxy:
    enable: true
    header: X-MyCustomHeader
```

`Chproxy` assumes the header contains the remote address and doesn't apply any parsing logic to extract the remote address from the header. 