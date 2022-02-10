---
title: Users
category: Configuration
position: 202
---

There are two types of users: `in-users` (in global section) and `out-users` (in cluster section).
This means all requests will be matched to `in-users` and if all checks are Ok - will be matched to `out-users`
with overriding credentials.

Suppose we have one ClickHouse user `web` with `read-only` permissions and `max_concurrent_queries: 4` limit.
There are two distinct applications `reading` from ClickHouse. We may create two distinct `in-users` with `to_user: "web"` and `max_concurrent_queries: 2` each in order to avoid situation when a single application exhausts all the 4-request limit on the `web` user.

Requests to `chproxy` must be authorized with credentials from [user_config](https://github.com/ContentSquare/chproxy/blob/master/config#user_config). Credentials can be passed via [BasicAuth](https://en.wikipedia.org/wiki/Basic_access_authentication) or via `user` and `password` [query string](https://en.wikipedia.org/wiki/Query_string) args.

Limits for `in-users` and `out-users` are independent.


