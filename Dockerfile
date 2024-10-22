ARG GO_VERSION=1.22
FROM golang:${GO_VERSION} AS build

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

EXPOSE 9090

ENTRYPOINT [ "/chproxy" ]
CMD [ "--help" ]
