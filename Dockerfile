FROM debian:12-slim

ARG USER=chproxy
ARG UID=1000
ARG GID=$UID

RUN groupadd -g $GID $USER && \
    useradd -u $UID -g $GID -m $USER && \
    apt update && \
    apt install -y ca-certificates curl

COPY chproxy /

EXPOSE 9090

USER $USER

ENTRYPOINT ["/chproxy"]

CMD ["--help"]
