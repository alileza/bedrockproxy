.PHONY: build build-frontend dev dev-frontend test clean

build: build-frontend
	go build -o bin/bedrockproxy .

build-frontend:
	cd web && pnpm install && pnpm exec vite build

dev:
	go run . -config config.yaml

dev-frontend:
	cd web && pnpm exec vite

test:
	go test ./...

clean:
	rm -rf bin/ web/dist/
