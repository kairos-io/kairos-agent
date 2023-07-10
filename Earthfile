VERSION 0.7
FROM alpine
# renovate: datasource=docker depName=golang
ARG --global GOLINT_VERSION=1.52.2
# renovate: datasource=docker depName=golang
ARG --global GO_VERSION=1.20-alpine3.17
# renovate: datasource=docker depName=cypress/base
ARG --global CYPRESS_VERSION=18.16.0

go-deps:
    ARG GO_VERSION
    FROM golang:$GO_VERSION
    WORKDIR /build
    COPY go.mod go.sum ./
    RUN go mod download
    SAVE ARTIFACT go.mod AS LOCAL go.mod
    SAVE ARTIFACT go.sum AS LOCAL go.sum

test:
    FROM +go-deps
    RUN apk add rsync gcc musl-dev bash
    WORKDIR /build
    COPY . .
    ARG TEST_PATHS=./...
    ARG LABEL_FILTER=
    ENV CGO_ENABLED=1
    RUN go run github.com/onsi/ginkgo/v2/ginkgo run --label-filter "$LABEL_FILTER" --covermode=atomic --coverprofile=coverage.out -v --race -r $TEST_PATHS
    SAVE ARTIFACT coverage.out AS LOCAL coverage.out

version:
    FROM alpine
    RUN apk add git
    COPY . ./
    RUN --no-cache echo $(git describe --always --tags --dirty) > VERSION
    RUN --no-cache echo $(git describe --always --dirty) > COMMIT
    ARG VERSION=$(cat VERSION)
    ARG COMMIT=$(cat COMMIT)
    SAVE ARTIFACT VERSION VERSION
    SAVE ARTIFACT COMMIT COMMIT

build-kairos-agent:
    FROM +go-deps
    RUN apk add upx
    COPY . .
    COPY +webui-deps/node_modules ./internal/webui/public/node_modules
    COPY github.com/kairos-io/kairos-docs:main+docs/public ./internal/webui/public/local
    COPY +version/VERSION ./
    COPY +version/COMMIT ./
    ARG VERSION=$(cat VERSION)
    ARG COMMIT=$(cat COMMIT)
    RUN --no-cache echo "Building Version: ${VERSION} and Commit: ${COMMIT}"
    ARG LDFLAGS="-s -w -X github.com/kairos-io/kairos-agent/v2/internal/common.VERSION=${VERSION} -X github.com/kairos-io/kairos-agent/v2/internal/common.gitCommit=$COMMIT"
    ENV CGO_ENABLED=${CGO_ENABLED}
    RUN go build -o kairos-agent -ldflags "${LDFLAGS}" main.go && upx kairos-agent
    SAVE ARTIFACT kairos-agent kairos-agent AS LOCAL build/kairos-agent

build:
    BUILD +build-kairos-agent

golint:
    ARG GO_VERSION
    FROM golang:$GO_VERSION
    ARG GOLINT_VERSION
    RUN wget -O- -nv https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s v$GOLINT_VERSION
    WORKDIR /build
    COPY . .
    RUN golangci-lint run

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