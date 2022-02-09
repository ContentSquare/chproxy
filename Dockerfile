FROM golang:1.17-alpine AS builder

RUN apk update && apk add --no-cache git ca-certificates && update-ca-certificates

FROM scratch

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

COPY chproxy /

ENTRYPOINT ["/chproxy"]
