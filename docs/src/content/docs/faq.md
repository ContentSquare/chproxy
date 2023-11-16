---
title: Frequently Asked Questions
---

* *Is `chproxy` production ready?*

  Yes, we successfully use it in production for `SELECT` requests. Others found it handy for `INSERT` as well. However, our benchmarks proved that it's better to insert data without proxy with `NATIVE` ClickHouse protocol.
  requests.

* *What about `chproxy` performance?*

  A single `chproxy` instance easily proxies 1Gbps of compressed `INSERT` data
  while using less than 20% of a single CPU core in our production setup.

* *Does `chproxy` support [native interface](https://clickhouse.com/docs/en/interfaces/tcp/) for ClickHouse?*

  No. Because currently all our services work with ClickHouse only via HTTP.
  Support for `native interface` may be added in the future.
