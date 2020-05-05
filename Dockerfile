FROM alpine:latest AS deploy
RUN apk --no-cache add ca-certificates
COPY calert /
COPY config.sample.toml  /etc/calert/config.toml
VOLUME ["/etc/calert"]
CMD ["./calert", "--config", "/etc/calert/config.toml"]  