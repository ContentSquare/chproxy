FROM golang:1.13-alpine AS build

RUN apk add --update zstd-static zstd-dev make gcc musl-dev git
RUN go get golang.org/x/lint/golint
RUN mkdir -p /go/src/github.com/Vertamedia/chproxy
WORKDIR /go/src/github.com/Vertamedia/chproxy
COPY . ./
ARG EXT_BUILD_TAG
ENV EXT_BUILD_TAG ${EXT_BUILD_TAG}
RUN make release-build

FROM alpine
COPY --from=build /go/src/github.com/Vertamedia/chproxy/chproxy /chproxy
ENTRYPOINT [ "/chproxy" ]
CMD [ "--help" ]
