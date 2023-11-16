---
title: Server
sidebar:
    order: 1
---

`Chproxy` may accept requests over `HTTP` and `HTTPS` protocols. [HTTPS](https://github.com/ContentSquare/chproxy/blob/master/config#https_config) must be configured with custom certificate or with automated [Let's Encrypt](https://letsencrypt.org/) certificates.

Access to `chproxy` can be limited by list of IPs or IP masks. This option can be applied to [HTTP](https://github.com/ContentSquare/chproxy/blob/master/config#http_config), [HTTPS](https://github.com/ContentSquare/chproxy/blob/master/config#https_config), [metrics](https://github.com/ContentSquare/chproxy/blob/master/config#metrics_config), [user](https://github.com/ContentSquare/chproxy/blob/master/config#user_config) or [cluster-user](https://github.com/ContentSquare/chproxy/blob/master/config#cluster_user_config).

