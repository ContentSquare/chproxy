---
title: Changelog
menuTitle: Changelog
category: Guides
position: 103
---

Since v1.17.1, chproxy follows [semantic versioning](https://semver.org/).
Don't expect breaking changes between 2 releases if they have the same major version.

### <a id="229"></a> release 1.20.0, 2022-11-??

#### Release note
* Until chproxy 1.19.0, the same cache was shared for all the users.
Since 1.20.0, each user has his own cache but you can override this behavior by setting `shared_with_all_users = true` in the config file.
* Since 1.20.0, if you're using docker images from [contentsquareplatform docker hub](https://hub.docker.com/r/contentsquareplatform/chproxy), the way to run them was simplified (cf `Improvment`)
* For security reason, since 1.20.0, only the clients using at least TLS 1.2 (released in 2008) can use chproxy with https.

#### New Feature
* Ability to decide whether the cache is specific per user or shared with all users (specific per user by default) [#258](https://github.com/ContentSquare/chproxy/pull/258) 
* Ability to retry a failed query up to `retrynumber` times [#242](https://github.com/ContentSquare/chproxy/pull/242) [#269](https://github.com/ContentSquare/chproxy/pull/269) [#270](https://github.com/ContentSquare/chproxy/pull/270)

#### Improvement
* add contribution guidelines [#257](https://github.com/ContentSquare/chproxy/pull/257)
* Since 1.20.0, the BINARY argument is not needed to run docker images from [contentsquareplatform docker hub](https://hub.docker.com/r/contentsquareplatform/chproxy)
```
#new way:
docker run -d -v $(PWD)/testdata:/opt/testdata/ contentsquareplatform/chproxy:v1.20.0-arm64v8 -config /opt/testdata/config.yml
#old way:
docker run -d -v $(PWD)/testdata:/opt/testdata/ -e BINARY=chproxy contentsquareplatform/chproxy:v1.19.0-arm64v8 -config /opt/testdata/config.yml
```
[270](https://github.com/ContentSquare/chproxy/pull/274)
Since 1.20.0, only the clients using at least TLS 1.2 (released in 2008) will be able to connect with chproxy in https [#276](https://github.com/ContentSquare/chproxy/pull/276)

#### Bug Fix
* By default the cache was shared with all users, which could led to situations where a user could access data he wasn't allowed to see (according to clickhouse rules). Now the cache is specific for each user [#258](https://github.com/ContentSquare/chproxy/pull/258) 



https://github.com/ContentSquare/chproxy/pull/258

### <a id="229"></a> release 1.19.0, 2022-10-23

#### New Feature
* Ability to run chproxy behind a reverse proxy (like cloudflare, nginx ...) [#225](https://github.com/ContentSquare/chproxy/pull/225) 
* Add changelog to follow the content of new releases [#253](https://github.com/ContentSquare/chproxy/pull/253)

#### Improvement
* Ability to use the wildcarded user feature using the patterns [some_prefix][\*], [\*][some_suffix] & [\*] instead of just [some_prefix]_[\*] [#250](https://github.com/ContentSquare/chproxy/pull/250)

#### Bug Fix
* The wildcarded user feature on 1.18.0 had a security issue that was fixed: If 2 different users connected to chproxy at the same time, sometimes the same user/pwd was used to query clickhouse [#250](https://github.com/ContentSquare/chproxy/pull/250)



### <a id="228"></a> release 1.18.0, 2022-10-13

#### New Feature
* (Wildcarded users) Ability to by-pass the user authentication of chproxy on rely only on clickhouse user authentication [#219](https://github.com/ContentSquare/chproxy/pull/219)
* add docker image for arm64v8 (for mac M1) [#247](https://github.com/ContentSquare/chproxy/pull/247)

#### Improvement
* When cache is activated and a query fails, the failure is cached just for 500 msec [#235](https://github.com/ContentSquare/chproxy/pull/235)
* Move from github.com/DataDog/zstd to github.com/klauspost/compress/zstd to avoid cgo dependencies to ease the use of chproxy on mac M1 [#238](https://github.com/ContentSquare/chproxy/pull/238)
* Ability to use a prometheus namespace for the exported metrics [#232](https://github.com/ContentSquare/chproxy/pull/232)

#### Bug Fix
* Fixed a few edge cases that could corrpupt the answer fetched from the redis cache [#244](https://github.com/ContentSquare/chproxy/pull/244)



### <a id="228"></a> release 1.17.2, 2022-09-15

#### Improvement
* Make the error more explicit (with the associated root cause) when cache is activated and a query fails then the same query is asked and the error is fetched from the cache [#229](https://github.com/ContentSquare/chproxy/pull/229)



### <a id="228"></a> release 1.17.1, 2022-09-12

#### Improvement
* Better selection of the errors that need to be cached to avoid the thundering herd problem [#193](https://github.com/ContentSquare/chproxy/pull/193)
* Ability to only cache queries on redis that are below a certain threshold [#191](https://github.com/ContentSquare/chproxy/pull/191)
* improve documentation [#215](https://github.com/ContentSquare/chproxy/pull/215)



### <a id="228"></a> release 1.17.0, 2022-08-29

#### Improvement
* improve processing speed when using cache  [#212](https://github.com/ContentSquare/chproxy/pull/212)
* improve chproxy.org design  [#213](https://github.com/ContentSquare/chproxy/pull/213)
* heartbeats to clickhouse now rely on /ping (by default) and longer need a user/pwd [#214](https://github.com/ContentSquare/chproxy/pull/214)

#### Bug Fix
* avoid memory issue when redis cache is used  [#212](https://github.com/ContentSquare/chproxy/pull/212)