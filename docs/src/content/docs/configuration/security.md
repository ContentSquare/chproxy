---
title: Security
sidebar:
    order: 4
---

`Chproxy` removes all the query params from input requests (except the user's [params](https://github.com/ContentSquare/chproxy/blob/master/config#param_groups_config) and listed [here](https://github.com/ContentSquare/chproxy/blob/master/scope.go#L292))
before proxying them to `ClickHouse` nodes. This prevents from unsafe overriding
of various `ClickHouse` [settings](https://clickhouse.com/docs/en/interfaces/http/).

Be careful when configuring limits, allowed networks, passwords etc.
By default `chproxy` tries detecting the most obvious configuration errors such as `allowed_networks: ["0.0.0.0/0"]` or sending passwords via unencrypted HTTP.

Special option `hack_me_please: true` may be used for disabling all the security-related checks during config validation (if you are feeling lucky :) ).

For sensitive configuration options, such as user passwords, chproxy supports loading configuration from environment variables. In order to load a configuration variable from a environment variable, a placeholder needs to be put in it's place
in the configuration file. Placeholder are of the form `${ENV_VAR_NAME}`. As an example, to load a user password from the environment variable `MY_PASSWORD` you can use a placeholder as in the following snippet:

```yaml
users:
  - name: "default"
    password: ${MY_PASSWORD}
```

This will be replaced by the actual environment variable once the configuration is (re)loaded from disk. If the environment variable isn't found the placeholder will remain and won't be replaced.
