---
title: Frequently Asked Questions
menuTitle: FAQ
category: Guides
position: 103
---

* *Is `chproxy` production ready?*

  Yes, we successfully use it in production for both `INSERT` and `SELECT`
  requests.

* *What about `chproxy` performance?*

  A single `chproxy` instance easily proxies 1Gbps of compressed `INSERT` data
  while using less than 20% of a single CPU core in our production setup.

* *Does `chproxy` support [native interface](http://clickhouse-docs.readthedocs.io/en/latest/interfaces/tcp.html) for ClickHouse?*

  No. Because currently all our services work with ClickHouse only via HTTP.
  Support for `native interface` may be added in the future.
