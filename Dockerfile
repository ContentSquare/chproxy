FROM debian

COPY chproxy /

EXPOSE 9090

ENTRYPOINT ["/chproxy"]
CMD [ "--help" ]
