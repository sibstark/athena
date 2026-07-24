ARG GO_IMAGE=golang:1.25.5-alpine3.22@sha256:3587db7cc96576822c606d119729370dbf581931c5f43ac6d3fa03ab4ed85a10
FROM ${GO_IMAGE} AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/rag-api ./cmd/server

FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/rag-api /rag-api
USER 65532:65532
EXPOSE 8080
HEALTHCHECK --interval=10s --timeout=5s --retries=6 --start-period=5s CMD ["/rag-api", "healthcheck"]
ENTRYPOINT ["/rag-api"]
