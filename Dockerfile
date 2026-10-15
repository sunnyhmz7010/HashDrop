# 静态前端资源与目标 CPU 架构无关，只在构建机架构上打包一次。
FROM --platform=$BUILDPLATFORM node:24-alpine AS frontend
WORKDIR /app/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

# 在构建机架构上运行 Go 编译器，避免 QEMU 模拟编译。
FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS builder
ARG TARGETOS
ARG TARGETARCH
WORKDIR /app
COPY . .
COPY --from=frontend /app/backend/web ./backend/web
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags="-s -w" -o /hashdrop ./backend

FROM alpine:3.24
RUN apk add --no-cache ca-certificates \
    && mkdir -p /data
COPY --from=builder /hashdrop /app/hashdrop
USER root
WORKDIR /app
CMD ["/app/hashdrop", "-config", "/data/config.json", "-data-dir", "/data", "-listen", "0.0.0.0:8080"]
