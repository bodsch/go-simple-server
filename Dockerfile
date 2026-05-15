# syntax=docker/dockerfile:1.7

# -----------------------------------------------------------------------------
# Stage 1: download wait4x for the target architecture and verify its checksum.
# Build host arch may differ from target arch (e.g. building arm64 on amd64),
# so we use the buildx-provided $TARGETARCH and grab the matching release.
# -----------------------------------------------------------------------------
FROM --platform=$BUILDPLATFORM mirror.gcr.io/alpine:3 AS downloader

ARG TARGETARCH
ARG VERSION_WAIT4X=3.6.0

RUN echo 'hosts: files dns' >> /etc/nsswitch.conf \
 && apk add --quiet --no-cache curl ca-certificates

WORKDIR /tmp

# Map Docker's TARGETARCH naming (amd64, arm64) to wait4x's release naming.
# wait4x publishes amd64 and arm64 binaries for linux; abort on others.
RUN set -eu; \
    case "${TARGETARCH}" in \
      amd64) ARCH=amd64 ;; \
      arm64) ARCH=arm64 ;; \
      *) echo "unsupported arch: ${TARGETARCH}" >&2; exit 1 ;; \
    esac; \
    FILE="wait4x-linux-${ARCH}.tar.gz"; \
    BASE="https://github.com/wait4x/wait4x/releases/download/v${VERSION_WAIT4X}"; \
    curl --fail --silent --show-error --location --output "${FILE}"            "${BASE}/${FILE}"; \
    curl --fail --silent --show-error --location --output "${FILE}.sha256sum"  "${BASE}/${FILE}.sha256sum"; \
    sha256sum -c "${FILE}.sha256sum"; \
    tar -xzf "${FILE}"; \
    mv wait4x /out-wait4x; \
    chmod 0755 /out-wait4x

# -----------------------------------------------------------------------------
# Stage 2: build the probe-service binary for the target architecture.
# We pin Go to 1.24 (matches go.mod) and produce a stripped, trimmed, static
# binary suitable for a scratch- or alpine-based runtime image.
# -----------------------------------------------------------------------------
FROM --platform=$BUILDPLATFORM mirror.gcr.io/golang:1.24-alpine AS build

ARG TARGETOS
ARG TARGETARCH

WORKDIR /src

# Cache module downloads in a dedicated layer. There are currently no
# external dependencies, so go.sum is absent; bind-mount any future
# go.sum at build time or replace this with an explicit COPY.
COPY go.mod ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" \
      -o /out/probe-service ./cmd/probe-service

# -----------------------------------------------------------------------------
# Stage 3: runtime image. Alpine is kept because the existing entrypoint.sh
# requires bash. A non-root user is created and used as the default.
# -----------------------------------------------------------------------------
FROM mirror.gcr.io/alpine:3

RUN echo 'hosts: files dns' >> /etc/nsswitch.conf \
 && apk add --quiet --no-cache bash ca-certificates tini \
 && addgroup -S app \
 && adduser -S -G app -u 10001 -h /home/app app

ENV PORT=8080
EXPOSE 8080

COPY rootfs /
COPY --from=build      /out/probe-service /usr/bin/probe-service
COPY --from=downloader /out-wait4x        /usr/bin/wait4x

RUN chmod 0755 /usr/bin/probe-service /usr/bin/wait4x /bin/entrypoint.sh

USER 10001:10001

# tini reaps zombies and forwards signals to the process tree, which matters
# when the entrypoint script execs another binary.
ENTRYPOINT ["/sbin/tini", "--", "/bin/entrypoint.sh"]
