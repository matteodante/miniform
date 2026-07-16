# syntax=docker/dockerfile:1.25.0@sha256:0adf442eae370b6087e08edc7c50b552d80ddf261576f4ebd6421006b2461f12

FROM golang:1.26.5-alpine@sha256:0178a641fbb4858c5f1b48e34bdaabe0350a330a1b1149aabd498d0699ff5fb2 AS builder

RUN apk add --no-cache build-base

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download && go mod verify

COPY cmd ./cmd
COPY internal ./internal
COPY web ./web

ARG VERSION=dev
ARG COMMIT_SHA=unknown

RUN CGO_ENABLED=1 GOOS=linux go build \
    -buildvcs=false \
    -trimpath \
    -ldflags="-s -w \
      -X main.version=${VERSION} \
      -X main.commit=${COMMIT_SHA} \
      -X github.com/matteodante/miniform/internal/server.buildCommit=${COMMIT_SHA}" \
    -o /out/miniform \
    ./cmd/miniform

FROM alpine:3.23@sha256:fd791d74b68913cbb027c6546007b3f0d3bc45125f797758156952bc2d6daf40

ARG VERSION=dev
ARG COMMIT_SHA=unknown

LABEL org.opencontainers.image.title="Miniform" \
      org.opencontainers.image.description="A quiet, self-hosted inbox for form submissions" \
      org.opencontainers.image.source="https://github.com/matteodante/miniform" \
      org.opencontainers.image.url="https://github.com/matteodante/miniform" \
      org.opencontainers.image.documentation="https://github.com/matteodante/miniform/blob/main/docs/installation.md" \
      org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${COMMIT_SHA}"

RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -g 10001 -S miniform \
    && adduser -u 10001 -S -D -H -G miniform miniform \
    && mkdir -p /app/storage/logs /usr/share/licenses/miniform \
    && chown -R miniform:miniform /app/storage

WORKDIR /app

COPY --from=builder /out/miniform /usr/local/bin/miniform
COPY LICENSE THIRD_PARTY_NOTICES.md /usr/share/licenses/miniform/

ENV MINIFORM_ENV=production \
    MINIFORM_PORT=8080 \
    MINIFORM_DATA_DIR=/app/storage \
    MINIFORM_LOGS_DIR=/app/storage/logs \
    MINIFORM_SESSION_TIMEOUT_SECONDS=1800

USER 10001:10001

EXPOSE 8080
VOLUME ["/app/storage"]

HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD wget --quiet --output-document=- "http://127.0.0.1:${MINIFORM_PORT}/_health" >/dev/null || exit 1

ENTRYPOINT ["/usr/local/bin/miniform"]
