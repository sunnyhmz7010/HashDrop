.PHONY: frontend build build-linux test e2e

frontend:
	npm --prefix frontend ci
	npm --prefix frontend run build

build: frontend
	go build -trimpath -ldflags="-s -w" -o bin/tinyfaka ./backend

build-linux: frontend
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o bin/tinyfaka-linux-amd64 ./backend

test:
	go test ./...
	npm --prefix frontend run check

e2e:
	cd frontend && npm run test:e2e
