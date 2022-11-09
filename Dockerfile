FROM ubuntu:22.04
RUN apt-get -y update && apt install -y ca-certificates

WORKDIR /app

COPY calert.bin .
COPY static/ /app/static/
COPY config.sample.toml config.toml

ARG CALERT_GID="999"
ARG CALERT_UID="999"

RUN addgroup --system --gid $CALERT_GID calert && \
    adduser --uid $CALERT_UID --system --ingroup calert calert && \
    chown -R calert:calert /app

USER calert
EXPOSE 6000

ENTRYPOINT [ "./calert.bin" ]
