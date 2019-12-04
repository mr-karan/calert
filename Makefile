.PHONY : build prod run fresh test clean docker-build docker-push

BIN := calert.bin

HASH := $(shell git rev-parse --short HEAD)
COMMIT_DATE := $(shell git show -s --format=%ci ${HASH})
BUILD_DATE := $(shell date '+%Y-%m-%d %H:%M:%S')
VERSION := ${HASH} (${COMMIT_DATE})
CALERT_IMAGE := mrkaran/calert
CALERT_TAG := 1.1.0

docker-build:
	docker build -t ${CALERT_IMAGE}:latest -t ${CALERT_IMAGE}:${CALERT_TAG} -f docker/Dockerfile .

docker-push:
	docker push ${CALERT_IMAGE}:latest
	docker push ${CALERT_IMAGE}:${CALERT_TAG}

build:
	go build -o ${BIN} -ldflags="-X 'main.version=${VERSION}' -X 'main.date=${BUILD_DATE}'"

run:
	./${BIN}

fresh: clean build run

test:
	go test

clean:
	go clean
	rm -f ${BIN}
