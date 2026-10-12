# syntax=docker/dockerfile:1.7
FROM node:22.20.0-bookworm-slim AS web
WORKDIR /src
RUN corepack enable
COPY web/package.json web/pnpm-lock.yaml ./web/
RUN corepack pnpm@10.18.0 --dir web install --frozen-lockfile
COPY web ./web
COPY internal/control/webui ./internal/control/webui
RUN corepack pnpm@10.18.0 --dir web build

FROM golang:1.24.9-bookworm AS build
ARG TARGETOS=linux
ARG TARGETARCH=amd64
ARG VERSION=dev
ARG COMMIT=unknown
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/internal/control/webui/dist ./internal/control/webui/dist
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -tags webui \
    -ldflags "-s -w -buildid= -X github.com/senhao-xu/vps-status/internal/versioninfo.Version=${VERSION} -X github.com/senhao-xu/vps-status/internal/versioninfo.Commit=${COMMIT}" \
    -o /out/vps-status-control ./cmd/control && mkdir -p /out/data && chmod 0700 /out/data

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/vps-status-control /vps-status-control
COPY --from=build --chown=nonroot:nonroot /out/data /data
VOLUME ["/data"]
EXPOSE 8080
USER nonroot:nonroot
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 CMD ["/vps-status-control", "healthcheck"]
ENTRYPOINT ["/vps-status-control"]
CMD ["--listen", ":8080", "--data-dir", "/data"]
