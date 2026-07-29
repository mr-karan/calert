FROM ubuntu:24.04

ARG TARGETPLATFORM
ARG CALERT_GID="999"
ARG CALERT_UID="999"

RUN apt-get update \
    && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends ca-certificates tzdata \
    && groupadd --gid "${CALERT_GID}" calert \
    && useradd --uid "${CALERT_UID}" --gid "${CALERT_GID}" --home-dir /app --no-create-home --shell /usr/sbin/nologin calert \
    && mkdir /app \
    && chown calert:calert /app \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY --chown=${CALERT_UID}:${CALERT_GID} ${TARGETPLATFORM}/calert.bin /app/calert.bin
COPY --chown=${CALERT_UID}:${CALERT_GID} static/ /app/static/
COPY --chown=${CALERT_UID}:${CALERT_GID} config.sample.toml /app/config.sample.toml

USER calert
EXPOSE 6000

ENTRYPOINT ["/app/calert.bin"]
