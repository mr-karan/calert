#!/usr/bin/env sh
set -eu

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
context_dir=$(mktemp -d)
containers=""

cleanup() {
    for container in $containers; do
        docker rm --force "$container" >/dev/null 2>&1 || :
    done
    rm -rf "$context_dir"
}

trap cleanup EXIT
trap 'cleanup; exit 1' HUP INT TERM

wait_for_ping() {
    container_name=$1
    endpoint=$2
    attempts=30

    while [ "$attempts" -gt 0 ]; do
        if curl --fail --silent --show-error --max-time 1 "$endpoint" >/dev/null; then
            return 0
        fi

        if [ "$(docker inspect --format '{{.State.Running}}' "$container_name")" != "true" ]; then
            docker logs "$container_name" >&2 || :
            return 1
        fi

        attempts=$((attempts - 1))
        sleep 1
    done

    docker logs "$container_name" >&2 || :
    return 1
}

run_smoke_image() {
    container_name=$1
    image=$2
    containers="$containers $container_name"
    docker rm --force "$container_name" >/dev/null 2>&1 || :

    if [ "${DOCKER_SMOKE_NETWORK:-}" = "host" ]; then
        docker run --detach --rm --name "$container_name" --network host "$image" >/dev/null
        wait_for_ping "$container_name" "http://127.0.0.1:6000/ping"
        docker rm --force "$container_name" >/dev/null
        return
    fi

    docker run --detach --rm --name "$container_name" --publish 127.0.0.1::6000 "$image" >/dev/null
    host_port=$(docker port "$container_name" 6000/tcp | sed 's/.*://')
    wait_for_ping "$container_name" "http://127.0.0.1:${host_port}/ping"
    docker rm --force "$container_name" >/dev/null
}

mkdir -p "$context_dir/linux/amd64"
(
    cd "$repo_root"
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o "$context_dir/linux/amd64/calert.bin" ./cmd/
)
cp "$repo_root/config.sample.toml" "$context_dir/config.sample.toml"
cp -R "$repo_root/static" "$context_dir/static"

docker build --network "${DOCKER_BUILD_NETWORK:-default}" --platform linux/amd64 --tag calert:smoke-ubuntu --file "$repo_root/Dockerfile" "$context_dir"
docker build --network "${DOCKER_BUILD_NETWORK:-default}" --platform linux/amd64 --tag calert:smoke-scratch --file "$repo_root/Dockerfile.scratch" "$context_dir"
docker run --rm --network none calert:smoke-ubuntu --help >/dev/null
docker run --rm --network none calert:smoke-scratch --help >/dev/null
run_smoke_image calert-smoke-ubuntu calert:smoke-ubuntu
run_smoke_image calert-smoke-scratch calert:smoke-scratch
