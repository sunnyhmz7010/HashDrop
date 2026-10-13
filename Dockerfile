FROM node:24-alpine AS frontend
WORKDIR /app/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

FROM golang:1.27-alpine AS builder
WORKDIR /app
COPY . .
COPY --from=frontend /app/backend/web ./backend/web
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /tinyfaka ./backend

FROM alpine:3.24
RUN apk add --no-cache ca-certificates \
    && addgroup -S -g 10001 tinyfaka \
    && adduser -S -D -u 10001 -G tinyfaka tinyfaka \
    && mkdir -p /etc/tinyfaka /var/lib/tinyfaka \
    && chown tinyfaka:tinyfaka /etc/tinyfaka /var/lib/tinyfaka
COPY --from=builder /tinyfaka /usr/local/bin/tinyfaka
USER tinyfaka
WORKDIR /var/lib/tinyfaka
CMD ["/usr/local/bin/tinyfaka", "-config", "/etc/tinyfaka/config.json", "-data-dir", "/var/lib/tinyfaka", "-listen", "0.0.0.0:8080"]
