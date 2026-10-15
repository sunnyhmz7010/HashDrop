FROM --platform=$BUILDPLATFORM node:24-alpine AS frontend
WORKDIR /app/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

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
