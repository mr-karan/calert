FROM ubuntu:24.04 AS certs

ARG CALERT_GID="999"
ARG CALERT_UID="999"

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates tzdata \
    && echo "calert:x:${CALERT_UID}:${CALERT_GID}::/home/calert:/sbin/nologin" > /etc/calert-passwd \
    && echo "calert:x:${CALERT_GID}:" > /etc/calert-group

FROM scratch

ARG CALERT_GID="999"
ARG CALERT_UID="999"

COPY --from=certs /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=certs /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

COPY --from=certs /etc/calert-passwd /etc/passwd
COPY --from=certs /etc/calert-group  /etc/group

WORKDIR /app

COPY calert.bin         .
COPY static/            /app/static/
COPY config.sample.toml config.toml

USER ${CALERT_UID}:${CALERT_GID}
EXPOSE 6000

ENTRYPOINT ["/app/calert.bin"]
