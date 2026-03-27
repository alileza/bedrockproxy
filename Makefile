.PHONY: build build-frontend dev dev-frontend test clean docker-up docker-down

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

docker-up:
	docker compose up -d

docker-down:
	docker compose down
