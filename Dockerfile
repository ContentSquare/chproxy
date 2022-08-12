FROM debian

ARG BINARY

COPY ${BINARY} /

EXPOSE 9090

ENTRYPOINT ["/chproxy"]
CMD [ "--help" ]
