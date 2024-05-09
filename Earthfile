VERSION 0.7
FROM alpine
# renovate: datasource=docker depName=golang
ARG --global GOLINT_VERSION=1.52.2
# renovate: datasource=docker depName=golang
ARG --global GO_VERSION=1.20-bookworm
# renovate: datasource=docker depName=cypress/base
ARG --global CYPRESS_VERSION=18.16.0

go-deps:
    ARG GO_VERSION
    FROM golang:$GO_VERSION
    RUN apt-get update && apt-get install -y rsync gcc bash git
    WORKDIR /build
    COPY . .
    RUN go mod tidy --compat=1.19
    RUN go mod download
    RUN go mod verify

test:
    FROM +go-deps
    WORKDIR /build
    ARG TEST_PATHS=./...
    ARG LABEL_FILTER=
    ENV CGO_ENABLED=1
    RUN go run github.com/onsi/ginkgo/v2/ginkgo run --label-filter "$LABEL_FILTER" --covermode=atomic --coverprofile=coverage.out -v --race -r $TEST_PATHS
    SAVE ARTIFACT coverage.out AS LOCAL coverage.out

version:
    FROM +go-deps
    RUN --no-cache echo $(git describe --always --tags --dirty) > VERSION
    RUN --no-cache echo $(git describe --always --dirty) > COMMIT
    ARG VERSION=$(cat VERSION)
    ARG COMMIT=$(cat COMMIT)
    SAVE ARTIFACT VERSION VERSION
    SAVE ARTIFACT COMMIT COMMIT

build-kairos-agent:
    FROM +go-deps
    COPY +webui-deps/node_modules ./internal/webui/public/node_modules
    COPY github.com/kairos-io/kairos-docs:main+docs/public ./internal/webui/public/local
    COPY +version/VERSION ./
    COPY +version/COMMIT ./
    ARG VERSION=$(cat VERSION)
    ARG COMMIT=$(cat COMMIT)
    RUN --no-cache echo "Building Version: ${VERSION} and Commit: ${COMMIT}"
    ARG LDFLAGS="-s -w -X github.com/kairos-io/kairos-agent/v2/internal/common.VERSION=${VERSION} -X github.com/kairos-io/kairos-agent/v2/internal/common.gitCommit=$COMMIT"
    ENV CGO_ENABLED=0
    RUN go build -o kairos-agent -ldflags "${LDFLAGS}" main.go
    SAVE ARTIFACT kairos-agent kairos-agent AS LOCAL build/kairos-agent

build:
    BUILD +build-kairos-agent

golint:
    FROM +go-deps
    ARG GOLINT_VERSION
    RUN wget -O- -nv https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s v$GOLINT_VERSION
    WORKDIR /build
    RUN bin/golangci-lint run

webui-deps:
    FROM node:19-alpine
    COPY . .
    WORKDIR ./internal/webui/public
    RUN npm install
    SAVE ARTIFACT node_modules /node_modules AS LOCAL internal/webui/public/node_modules

webui-tests:
    FROM cypress/base:$CYPRESS_VERSION
    COPY +build-kairos-agent/kairos-agent /usr/bin/kairos-agent
    COPY . src/
    WORKDIR src/
    RUN .github/cypress_tests.sh
    SAVE ARTIFACT /src/internal/webui/public/cypress/videos videos