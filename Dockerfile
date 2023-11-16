FROM debian

RUN apt update && apt install -y ca-certificates curl

COPY chproxy /

EXPOSE 9090

ENTRYPOINT ["/chproxy"]
CMD [ "--help" ]
