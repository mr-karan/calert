FROM ubuntu:20.04
WORKDIR /app
COPY calert.bin .
COPY static/ /app/static/
COPY config.sample.toml config.toml
ENTRYPOINT [ "./calert.bin" ]
