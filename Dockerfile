FROM ubuntu:20.04
RUN apt-get -y update && apt install -y ca-certificates
WORKDIR /app
COPY calert.bin .
COPY static/ /app/static/
COPY config.sample.toml config.toml
ENTRYPOINT [ "./calert.bin" ]
