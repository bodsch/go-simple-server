# syntax=docker/dockerfile:1

FROM mirror.gcr.io/alpine:3 AS downloader

ARG VERSION_WAIT4X=3.6.0

RUN \
  apk add \
    curl

WORKDIR /tmp

# install wait4x
RUN \
  curl \
    --silent \
    --location \
    --output wait4x-linux-amd64.tar.gz \
    "https://github.com/wait4x/wait4x/releases/download/v${VERSION_WAIT4X}/wait4x-linux-amd64.tar.gz"

RUN \
  curl \
    --silent \
    --location \
    --output wait4x-linux-amd64.tar.gz.sha256sum \
    "https://github.com/wait4x/wait4x/releases/download/v${VERSION_WAIT4X}/wait4x-linux-amd64.tar.gz.sha256sum"

RUN \
  set -e; \
  sha256sum -c wait4x-linux-amd64.tar.gz.sha256sum && \
  tar -xzf wait4x-linux-amd64.tar.gz

# -------------------------------

FROM mirror.gcr.io/golang:1.22-alpine AS build

WORKDIR /tmp
COPY go.mod ./
RUN go mod download

COPY cmd/ ./cmd/

RUN \
  CGO_ENABLED=0 GOOS=linux GOARCH=amd64; \
  go build -trimpath -ldflags="-s -w" -o /out/simple-api ./cmd/server

# -------------------------------

FROM mirror.gcr.io/alpine:3

RUN \
  apk add \
    bash

ENV PORT=8080
EXPOSE 8080
COPY rootfs /
COPY --from=build /out/simple-api /usr/bin/simple-api
COPY --from=downloader /tmp/wait4x /usr/bin/wait4x

# USER nonroot:nonroot

CMD ["/bin/wait4x"]

ENTRYPOINT ["/bin/entrypoint.sh"]
