VERSION ?= dev

.PHONY: ui build run dev test clean

ui:
	cd ui && npm ci && npm run build

build: ui
	CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o bin/homie ./cmd/homie

test:
	go test ./...

run: build
	HOMIE_DATA=./data ./bin/homie

# Sviluppo: backend su :8080, interfaccia con ricaricamento automatico su :5173
dev:
	HOMIE_DATA=./data go run ./cmd/homie & cd ui && npm run dev

clean:
	rm -rf bin data internal/webui/dist/app
