ARG GO_VERSION=1.22.7
FROM golang:${GO_VERSION}-alpine AS build

RUN apk add --no-cache --update zstd-static zstd-dev make gcc musl-dev git libc6-compat && \
    apk del openssl && \
    apk add --no-cache openssl=3.3.2-r1

RUN mkdir -p /go/src/github.com/contentsquare/chproxy
WORKDIR /go/src/github.com/contentsquare/chproxy
COPY . ./
ARG EXT_BUILD_TAG
ENV EXT_BUILD_TAG=${EXT_BUILD_TAG}
RUN make release-build
RUN ls -al /go/src/github.com/contentsquare/chproxy

FROM alpine
RUN apk add --no-cache curl ca-certificates
COPY --from=build /go/src/github.com/contentsquare/chproxy/chproxy* /

ENTRYPOINT [ "/chproxy" ]
CMD [ "--help" ]
