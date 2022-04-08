---
title: Installation
category: Guides
position: 102
---

### Precompiled binaries

Precompiled `chproxy` binaries are available [here](https://github.com/ContentSquare/chproxy/releases).
Just download the latest stable binary, unpack and run it with the desired [config](/configuration/default):

```
./chproxy -config=/path/to/config.yml
```

### Building from source

Chproxy is written in [Go](https://golang.org/). The easiest way to install it from sources is:

```
go get -u github.com/ContentSquare/chproxy
```

If you don't have Go installed on your system - follow [this guide](https://golang.org/doc/install).


### Docker

Chproxy is also available as a Docker image in a public repository on DockerHub.

Download the image and run the container:

```
docker run -d -v <LOCAL CONFIG>:/config.yml contentsquareplatform/chproxy:<VERSION TAG> -config /config.yml
```

Example:

```
docker run -d -v $(pwd)/config/examples/simple.yml:/config.yml contentsquareplatform/chproxy:1.15.0 -config /config.yml
```
