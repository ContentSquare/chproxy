---
title: Users
sidebar:
    order: 2
---

There are two types of users: `in-users` (in global section) and `out-users` (in cluster section).
This means all requests will be matched to `in-users` and if all checks are Ok - will be matched to `out-users`
with overriding credentials.

Suppose we have one ClickHouse user `web` with `read-only` permissions and `max_concurrent_queries: 4` limit.
There are two distinct applications `reading` from ClickHouse. We may create two distinct `in-users` with `to_user: "web"` and `max_concurrent_queries: 2` each in order to avoid situation when a single application exhausts all the 4-request limit on the `web` user.

Requests to `chproxy` must be authorized with credentials from [user_config](https://github.com/ContentSquare/chproxy/blob/master/config#user_config). Credentials can be passed via [BasicAuth](https://en.wikipedia.org/wiki/Basic_access_authentication) or via `user` and `password` [query string](https://en.wikipedia.org/wiki/Query_string) args.

Limits for `in-users` and `out-users` are independent.


`in-users` with `is_wildcarded` flag `true` can bypass chproxy authentication. In this case, the name plays the role of a pattern and must either look like
* `<prefix>*` (e.g `analyst_*`)
* `*<suffix>` (e.g `*-UK`)
* `*`
The asterisk matches a sequence of valid characters (except asterisk) in user name in request to `chproxy`. `chproxy` serves wildcarded users in a normal way except their credentials from incoming requests are resent to ClickHouse as they are and chproxy doesn't try to authenticate them. Passwords and names of `out-users` are not used for communications with ClickHouse. For security reasons, the default user (that should be disabled in production clickhouse servers) can't work with the wildcarded feature. So, even if you have an `*` wildcarded user, if someone uses chproxy with the user/pwd "default"/"", the query won't go to clickhouse. If you want to use the default user, you have to create a specific default user.

The current implementation of wildcarded doesn't work with some limits on `out-users` (like the max_concurrent_queries) but works well with all the limits on `in-users`. The limits for `in_users` and `out-users` works as if all the users matching the pattern were the same user.

If the wildcarded users are overlapping, the real users will be attached randomly to one of the wildcarded users. For example, let's say:
* there are 2 wildcarded users analyst_* and *-UK
* the user analyst_john-UK is using chproxy
analyst_john-UK will be attached either to analyst_* or *-UK. And, even if it is attached to analyst_*, it could be attached to *-UK for its next query. This could have an impact on user limitations and caching.