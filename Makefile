.PHONY : build prod run fresh test clean

BIN := calert.bin

HASH := $(shell git rev-parse --short HEAD)
COMMIT_DATE := $(shell git show -s --format=%ci ${HASH})
BUILD_DATE := $(shell date '+%Y-%m-%d %H:%M:%S')
VERSION := ${HASH} (${COMMIT_DATE})


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
