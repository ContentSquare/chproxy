---
title: Installation
---
### Precompiled binaries

Precompiled `chproxy` binaries are available [here](https://github.com/ContentSquare/chproxy/releases).
Just download the latest stable binary, unpack and run it with the desired [config](/configuration/default):

```console
./chproxy -config=/path/to/config.yml
```

### Building from source

Chproxy is written in [Go](https://golang.org/). The easiest way to install it from sources is:

```console
go get -u github.com/ContentSquare/chproxy
```

If you don't have Go installed on your system - follow [this guide](https://golang.org/doc/install).


### Docker

Chproxy is also available as a Docker image in a public repository on DockerHub.

Download the image and run the container:

```console
docker run -d -v <LOCAL CONFIG>:/config.yml contentsquareplatform/chproxy:<VERSION TAG> -config /config.yml
```

Example:

```console
docker run -d -v $(pwd)/config/examples/simple.yml:/config.yml contentsquareplatform/chproxy:v1.20.0 -config /config.yml
```

### Build a local image.

To build an image, as a prerequisite, the `chproxy` binary should exist in the same context as Dockerfile.

You can download the prebuilt version or run the following command:
```console
GOOS=linux GOARCH=amd64 go build -v
```

Then run docker build command:
```console
docker build -t <LOCAL_IMAGE_NAME> .
```

Finally, run container:
```console
docker run -d -v <LOCAL_VOLUME>:<DEST_PATH> -p <SOURCE_PORT>:<DEST_PORT> <LOCAL_IMAGE_NAME> <APPLICATION_ARGS>
```

Flags
```text
-d Run container in background and print container ID
-v Bind mount a volume (local directory or file to host path)
-p Publish a container's port(s) to the host (default 9090)
```

Example:
```console
docker run -d -v $(PWD)/config.yml:/opt/config.yml -p 9090:9090 chproxy-test -config /opt/config.yml
```
