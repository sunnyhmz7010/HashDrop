# syntax=docker/dockerfile:1

# 前端与 Go 构建在构建机架构运行，避免在 QEMU 中执行编译器。
FROM --platform=$BUILDPLATFORM node:24-alpine AS frontend
WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci
COPY frontend/ ./
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS backend
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY assets.go ./
COPY backend/*.go ./backend/
COPY migrations/ ./migrations/
COPY --from=frontend /src/backend/web/ ./backend/web/
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    go test ./... && go vet ./...
ARG TARGETOS
ARG TARGETARCH
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /out/tinyfaka ./backend

FROM alpine:3.24
RUN apk add --no-cache ca-certificates \
    && addgroup -S -g 10001 tinyfaka \
    && adduser -S -D -u 10001 -G tinyfaka tinyfaka \
    && mkdir -p /etc/tinyfaka /var/lib/tinyfaka \
    && chown tinyfaka:tinyfaka /etc/tinyfaka /var/lib/tinyfaka
COPY --from=backend /out/tinyfaka /usr/local/bin/tinyfaka
USER 10001:10001
WORKDIR /var/lib/tinyfaka
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
    CMD ["/usr/local/bin/tinyfaka", "-healthcheck", "-config", "/etc/tinyfaka/config.json"]
ENTRYPOINT ["/usr/local/bin/tinyfaka"]
CMD ["-config", "/etc/tinyfaka/config.json", "-data-dir", "/var/lib/tinyfaka", "-listen", "0.0.0.0:8080"]
