FROM debian:13-slim

RUN apt-get update && \
    apt-get upgrade -y && \
    apt-get install -y --no-install-recommends ca-certificates curl && \
    rm -rf /var/lib/apt/lists/*

COPY chproxy /

EXPOSE 9090

ENTRYPOINT ["/chproxy"]
CMD [ "--help" ]
