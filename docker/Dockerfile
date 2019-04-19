ARG GO_VERSION=1.12
FROM golang:${GO_VERSION}-alpine AS builder
RUN apk update && apk add gcc libc-dev make git
WORKDIR /calert/
COPY ./ ./
ENV CGO_ENABLED=0 GOFLAGS=-mod=vendor GOOS=linux
RUN make build

FROM alpine:latest AS deploy
RUN apk --no-cache add ca-certificates
WORKDIR /calert/
COPY --from=builder /calert/ ./
RUN mkdir -p /etc/calert && cp config.toml.sample /etc/calert/config.toml
# Define data volumes
VOLUME ["/etc/calert"]
CMD ["./calert.bin", "--config.file", "/etc/calert/config.toml"]  

