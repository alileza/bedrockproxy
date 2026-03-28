.PHONY: build generate dev dev-frontend test clean

build:
	go generate .
	go build -o bin/bedrockproxy .

generate:
	go generate .

dev:
	go run . -config config.yaml

dev-frontend:
	cd web && pnpm exec vite

test:
	go test ./...

clean:
	rm -rf bin/ dist/ web/dist/
