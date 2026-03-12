# syntax=docker/dockerfile:1

FROM mirror.gcr.io/alpine:3 AS downloader

ARG VERSION_WAIT4X=3.6.0

RUN \
  echo 'hosts: files dns' >> /etc/nsswitch.conf && \
  apk update  --quiet --no-cache && \
  apk upgrade --quiet --no-cache && \
  apk add     --quiet --no-cache \
    curl \
    ca-certificates

WORKDIR /tmp

# install wait4x
RUN \
  curl \
    --verbose \
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

FROM mirror.gcr.io/golang:1.23-alpine AS build

WORKDIR /src

COPY go.mod ./
RUN go mod download

# now copy the rest
COPY . .

RUN \
  CGO_ENABLED=0 GOOS=linux GOARCH=amd64; \
  go build -trimpath -ldflags="-s -w" -o /out/probe-service ./cmd/probe-service

# -------------------------------

FROM mirror.gcr.io/alpine:3

RUN \
  echo 'hosts: files dns' >> /etc/nsswitch.conf && \
  apk update  --quiet --no-cache && \
  apk upgrade --quiet --no-cache && \
  apk add     --quiet --no-cache \
    bash \
    ca-certificates

ENV PORT=8080
EXPOSE 8080
COPY rootfs /
COPY --from=build /out/probe-service /usr/bin/probe-service
COPY --from=downloader /tmp/wait4x /usr/bin/wait4x
# USER nonroot:nonroot
ENTRYPOINT ["/bin/entrypoint.sh"]
# CMD ["/bin/wait4x"]

