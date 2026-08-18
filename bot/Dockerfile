ARG GO_IMAGE=golang:1.26.4-alpine3.23
ARG ALPINE_IMAGE=alpine:3.23
FROM ${GO_IMAGE} AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/bot ./cmd

FROM ${ALPINE_IMAGE}

RUN apk add --no-cache ca-certificates \
    && addgroup -g 10001 -S athena \
    && adduser -u 10001 -S -G athena athena \
    && mkdir -p /app/logs \
    && chown -R athena:athena /app

WORKDIR /app
COPY --from=build --chown=athena:athena /out/bot /app/bot

USER athena
ENV LOG_DIR=/app/logs

EXPOSE 8080
HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=6 \
  CMD wget -q -O - http://127.0.0.1:${PORT:-8080}/health/live >/dev/null || exit 1

ENTRYPOINT ["/app/bot"]
