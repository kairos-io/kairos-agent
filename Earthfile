VERSION 0.7
FROM alpine
# renovate: datasource=docker depName=golang
ARG --global GOLINT_VERSION=1.52.2
# renovate: datasource=docker depName=golang
ARG --global GO_VERSION=1.20-alpine3.17

go-deps:
    ARG GO_VERSION
    FROM golang:$GO_VERSION
    WORKDIR /build
    COPY go.mod go.sum ./
    RUN go mod download
    SAVE ARTIFACT go.mod AS LOCAL go.mod
    SAVE ARTIFACT go.sum AS LOCAL go.sum

luet:
    FROM quay.io/luet/base:0.34.0
    SAVE ARTIFACT /usr/bin/luet /luet

test:
    FROM +go-deps
    RUN apk add rsync gcc musl-dev docker jq bash
    WORKDIR /build
    COPY +luet/luet /usr/bin/luet
    COPY . .
    # Some test require the docker sock exposed
    ARG TEST_PATHS=./...
    ARG LABEL_FILTER=
    ENV CGO_ENABLED=1
    WITH DOCKER
        RUN go run github.com/onsi/ginkgo/v2/ginkgo run --label-filter "$LABEL_FILTER" -v --output-interceptor-mode=none --fail-fast --race --covermode=atomic --coverprofile=coverage.out -r $TEST_PATHS
    END
    SAVE ARTIFACT coverage.out AS LOCAL coverage.out

version:
    FROM alpine
    RUN apk add git

    COPY . ./

    RUN --no-cache echo $(git describe --always --tags --dirty) > VERSION

    ARG VERSION=$(cat VERSION)
    SAVE ARTIFACT VERSION VERSION

build-kairos-agent:
    FROM +go-deps
    RUN apk add upx
    COPY . .
    COPY +webui-deps/node_modules ./internal/webui/public/node_modules
    COPY github.com/kairos-io/kairos:master+docs/public/local ./internal/webui/public/local
    COPY +version/VERSION ./
    ARG VERSION=$(cat VERSION)
    RUN echo $(cat VERSION)
    ARG LDFLAGS="-s -w -X 'github.com/kairos-io/kairos/v2/internal/common.VERSION=${VERSION}'"
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
    FROM ubuntu:22.10
    RUN apt-get update && apt-get install -y libgtk2.0-0 libgtk-3-0 libgbm-dev libnotify-dev libgconf-2-4 libnss3 libxss1 libasound2 libxtst6 xauth xvfb golang nodejs npm
    COPY +build-kairos-agent/kairos-agent /usr/bin/kairos-agent
    COPY . src/
    WORKDIR src/
    RUN .github/cypress_tests.sh
    SAVE ARTIFACT /src/internal/webui/public/cypress/videos videos