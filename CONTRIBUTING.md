# Contributing

## Prerequisites

- Go 1.24+
- Node.js 22+
- pnpm 10+

## Setup

```bash
git clone git@github.com:alileza/bedrockproxy.git
cd bedrockproxy
go generate .
go build .
```

## Development

```bash
# Terminal 1: Go backend
make dev

# Terminal 2: React frontend (hot reload)
make dev-frontend
```

Backend runs on `:8080`, frontend on `:5173` (proxies API calls to backend).

## Project structure

```
bedrockproxy/
├── main.go                    # Entrypoint, go:generate, go:embed
├── config.yaml                # Runtime config
├── internal/
│   ├── api/                   # HTTP routes, WebSocket, frontend serving
│   ├── auth/                  # SigV4 parsing, STS identity resolution
│   ├── config/                # YAML config loader
│   ├── metrics/               # Prometheus metric definitions
│   ├── pricing/               # Auto-discovery from AWS Pricing API
│   ├── proxy/                 # Transparent HTTP reverse proxy to Bedrock
│   ├── quota/                 # Per-caller quota engine
│   ├── store/                 # In-memory store, S3 flusher
│   └── usage/                 # Request tracking, cost calculation
└── web/                       # React + TypeScript + Tailwind
    └── src/
        ├── components/        # Table, StatCard, QuotaSection, etc.
        ├── hooks/             # useWS, useHighlight, useTheme
        ├── api/               # Typed API client
        └── lib/               # Formatters
```

## Build

```bash
make build          # go generate + go build → bin/bedrockproxy
```

`go generate` builds the frontend and copies it to `dist/`, which `go:embed` bakes into the binary.

## Frontend

Design tokens are in `web/src/index.css`. Supports dark mode.

Key rules:
- Use the existing color/spacing/radius tokens — don't introduce new ones
- API types live in `src/api/client.ts`
- All API queries use TanStack Query with auto-refresh via WebSocket

## Backend

Standard Go project. No frameworks — just `net/http` and the AWS SDK.

Key rules:
- The proxy is a transparent HTTP reverse proxy — it doesn't parse Bedrock-specific request/response formats
- All state lives in `internal/store/` — no database
- The store must be thread-safe (`sync.RWMutex`)
- Caller identity extracted from SigV4 headers, resolved via STS

## Tests

```bash
go test ./...           # 80+ tests
go test -race ./...     # with race detector
```

## Commits

- Keep commits focused — one thing per commit
- Use imperative mood: "Add feature" not "Added feature"
