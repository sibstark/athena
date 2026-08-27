ARG GO_IMAGE=golang:1.26.4-alpine3.23
FROM ${GO_IMAGE} AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/server

FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/api /api
USER 65532:65532
EXPOSE 8080
HEALTHCHECK --interval=10s --timeout=5s --retries=6 --start-period=5s CMD ["/api", "healthcheck"]
ENTRYPOINT ["/api"]
