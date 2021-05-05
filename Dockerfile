FROM alpine:latest AS deploy
RUN apk --no-cache add ca-certificates
WORKDIR /app
COPY calert .
COPY config.sample.toml  /etc/calert/config.toml
COPY message.tmpl /etc/calert/message.tmpl
VOLUME ["/etc/calert"]
CMD ["./calert", "--config.file", "/etc/calert/config.toml"]
