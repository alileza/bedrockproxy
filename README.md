<p><img src="web/public/logo.svg" width="48" align="left" style="margin-right: 12px" /> <strong style="font-size: 2em">bedrockproxy</strong></p>

<br clear="left" />

A thin proxy in front of AWS Bedrock that tracks who's using what, how much it costs, and shows it all in a real-time dashboard.

**Problem**: AWS Bedrock has no per-IAM-role usage analytics. CloudWatch metrics only break down by model, not by caller. You can't answer "how much did team X spend on Claude this week?"

**Solution**: Point your AWS SDK at bedrockproxy instead of Bedrock. Same auth, same API. The proxy forwards everything to Bedrock and tracks usage per caller.

![Dashboard](docs/dashboard.png)

## Why not LiteLLM / Bifrost / etc?

Those are great tools, but they solve a different problem.

|  | bedrockproxy | LiteLLM / others |
|---|---|---|
| **Migration cost** | Change one line: `endpoint_url` | New SDK integration, new API keys, new auth flow per service |
| **Operational cost** | Single binary, ~20MB RAM, no database | Postgres + Redis + Python runtime |
| **Infrastructure** | One pod on EKS, that's it | Database provisioning, connection pooling, migrations, backups |
| **Auth model** | Your existing IAM roles, unchanged | Issue and manage virtual API keys per team |
| **Identity tracking** | Real AWS IAM role ARNs | Virtual key labels |
| **Time to value** | Deploy → see data in minutes | Weeks of integration across services |

### When to use what

**Use bedrockproxy** if you're on AWS Bedrock and want visibility into who's calling what without touching any client code. Deploy it, change the endpoint URL, done.

**Use LiteLLM** if you need multi-provider routing (OpenAI + Anthropic + Bedrock), budget enforcement, or OpenAI-compatible API translation.

## Supported Bedrock operations

The proxy is a transparent HTTP reverse proxy — it forwards **all** Bedrock Runtime operations:

| Operation | Path | Streaming |
|---|---|---|
| Converse | `/model/{id}/converse` | No |
| ConverseStream | `/model/{id}/converse-stream` | Yes |
| InvokeModel | `/model/{id}/invoke` | No |
| InvokeModelWithResponseStream | `/model/{id}/invoke-with-response-stream` | Yes |
| CountTokens | `/model/{id}/count-tokens` | No |
| ApplyGuardrail | `/guardrail/{id}/version/{ver}/apply` | No |
| StartAsyncInvoke | `/async-invoke` | No |
| ListAsyncInvokes | `GET /async-invoke` | No |
| GetAsyncInvoke | `GET /async-invoke/{arn}` | No |

Works with Claude Code, Cline, LangChain, boto3, AWS CLI — anything that talks to Bedrock.

## How it works

```
Your app (AWS SDK)                    bedrockproxy                     AWS Bedrock
     │                                     │                               │
     │── any Bedrock request ────────────▶│                               │
     │   (SigV4 signed, same as usual)     │── re-sign + forward ────────▶│
     │                                     │◀── stream response back ─────│
     │◀── response (unchanged) ───────────│                               │
     │                                     │── record: caller, model,      │
     │                                     │   tokens, cost, latency       │
     │                                     │── notify dashboard (websocket)│
```

## Usage

```bash
# Start the proxy
make dev

# Use it — just add --endpoint-url
aws bedrock-runtime converse \
    --endpoint-url http://localhost:8080 \
    --model-id eu.anthropic.claude-sonnet-4-6 \
    --messages '[{"role":"user","content":[{"text":"Hello"}]}]' \
    --region eu-central-1

# Or in Python
import boto3
client = boto3.client("bedrock-runtime", endpoint_url="http://localhost:8080")

# Works with Claude Code too
export CLAUDE_CODE_USE_BEDROCK=1
export BEDROCK_ENDPOINT_URL=http://bedrockproxy.internal:8080
```

No new API keys. No new auth. Just change the endpoint URL.

## Deploying on EKS

### IAM policy

The proxy pod needs an IRSA role with these permissions:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Sid": "BedrockInvoke",
      "Effect": "Allow",
      "Action": [
        "bedrock:InvokeModel",
        "bedrock:InvokeModelWithResponseStream"
      ],
      "Resource": [
        "arn:aws:bedrock:*:*:inference-profile/*",
        "arn:aws:bedrock:*::foundation-model/*"
      ]
    },
    {
      "Sid": "IdentityResolution",
      "Effect": "Allow",
      "Action": "sts:GetAccessKeyInfo",
      "Resource": "*"
    },
    {
      "Sid": "PricingDiscovery",
      "Effect": "Allow",
      "Action": [
        "bedrock:ListFoundationModels",
        "pricing:GetProducts"
      ],
      "Resource": "*"
    },
    {
      "Sid": "S3Flush",
      "Effect": "Allow",
      "Action": "s3:PutObject",
      "Resource": "arn:aws:s3:::YOUR_BUCKET/bedrockproxy/*",
      "Condition": {
        "StringEquals": { "Note": "Only needed if S3 flushing is enabled" }
      }
    }
  ]
}
```

### Kubernetes manifest

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: bedrockproxy
spec:
  replicas: 1
  selector:
    matchLabels:
      app: bedrockproxy
  template:
    metadata:
      labels:
        app: bedrockproxy
      annotations:
        prometheus.io/scrape: "true"
        prometheus.io/port: "8080"
        prometheus.io/path: "/metrics"
    spec:
      serviceAccountName: bedrockproxy  # IRSA-annotated
      containers:
        - name: bedrockproxy
          image: ghcr.io/alileza/bedrockproxy:latest
          args: ["-config", "/config/config.yaml"]
          ports:
            - containerPort: 8080
          volumeMounts:
            - name: config
              mountPath: /config
            - name: data
              mountPath: /data
          env:
            - name: AWS_REGION
              value: "eu-central-1"
          livenessProbe:
            httpGet:
              path: /api/health
              port: 8080
          readinessProbe:
            httpGet:
              path: /api/health
              port: 8080
      volumes:
        - name: config
          configMap:
            name: bedrockproxy-config
        - name: data
          persistentVolumeClaim:
            claimName: bedrockproxy-data  # for .bedrockproxy-callers.json + quotas
---
apiVersion: v1
kind: Service
metadata:
  name: bedrockproxy
spec:
  selector:
    app: bedrockproxy
  ports:
    - port: 8080
```

Clients then set `endpoint_url` to `http://bedrockproxy.8080`.

## Quotas

Configure per-caller usage limits in `config.yaml`:

```yaml
quotas:
  - id: "staging-account"
    match: "account:590211171383"     # all callers from this AWS account
    cost_per_day: 10.0                # $10/day cap
    tokens_per_day: 1000000           # 1M tokens/day
    requests_per_minute: 60
    mode: "warn"                      # warn | reject
    enabled: true

  - id: "production-queryhub"
    match: "arn:aws:sts::830006442260:assumed-role/queryhub-*"  # glob on ARN
    cost_per_day: 100.0
    mode: "reject"                    # returns 429 when exceeded
    enabled: true

  - id: "default"
    match: "*"                        # catch-all
    cost_per_day: 5.0
    mode: "warn"
    enabled: true
```

Match patterns:
- `account:<id>` — all callers from an AWS account
- `arn:aws:sts::*:assumed-role/MyRole*` — glob on the caller's IAM role ARN
- `*` — catch-all default

Modes:
- `warn` — log + Prometheus metric, request goes through
- `reject` — return HTTP 429 Too Many Requests

Quotas can be overridden at runtime via `POST /api/quotas` (persisted to `.bedrockproxy-quotas.json`). The dashboard shows live usage progress bars per quota.

## Configuration

```yaml
server:
  port: 8080

aws:
  region: "eu-central-1"

s3:
  bucket: ""          # leave empty to disable S3 flushing
  prefix: "bedrockproxy"
  flush_interval: "5m"

# Models are auto-discovered from AWS. Manual override:
# models:
#   - id: "anthropic.claude-sonnet-4-6"
#     input_price_per_million: 3.0
#     output_price_per_million: 15.0

quotas: []  # see Quotas section above
```

## Architecture

```
                    ┌─────────────────────┐
                    │    bedrockproxy      │
                    │                     │
  AWS SDK ────────▶ │  HTTP reverse proxy  │ ────────▶ AWS Bedrock
                    │  SigV4 re-signing    │
                    │  In-memory store     │ ────────▶ S3 (periodic flush)
                    │  Quota engine        │
                    │  WebSocket events    │
                    │  Prometheus /metrics  │
                    │  Embedded React UI   │
                    └─────────────────────┘
                         single binary
```

- **Transparent proxy** — forwards all Bedrock operations, including streaming
- **In-memory store** — no database needed
- **~16MB** compiled binary

## Prometheus metrics

| Metric | Type | Labels |
|---|---|---|
| `bedrockproxy_requests_total` | counter | model, operation, status |
| `bedrockproxy_request_duration_seconds` | histogram | model, operation |
| `bedrockproxy_input_tokens_total` | counter | model, caller |
| `bedrockproxy_output_tokens_total` | counter | model, caller |
| `bedrockproxy_cost_usd_total` | counter | model, caller |
| `bedrockproxy_active_requests` | gauge | |
| `bedrockproxy_websocket_clients` | gauge | |
| `bedrockproxy_quota_exceeded_total` | counter | quota_id, mode, caller |

## Development

```bash
make dev            # Go backend on :8080
make dev-frontend   # Vite HMR on :5173

make build          # go generate + go build → bin/bedrockproxy
go test -race ./... # 80+ tests, race-detector clean
```

## Docker

```bash
docker run -p 8080:8080 -v $(pwd)/config.yaml:/config.yaml \
  ghcr.io/alileza/bedrockproxy -config /config.yaml
```

## Caller identity

The proxy extracts the caller's access key from the SigV4 Authorization header and resolves the AWS account via `sts:GetAccessKeyInfo`.

For full IAM role ARN display, register once per account:

```bash
ARN=$(aws sts get-caller-identity --query Arn --output text)
curl -X POST http://localhost:8080/api/register-caller \
  -H "Content-Type: application/json" \
  -H "Authorization: AWS4-HMAC-SHA256 Credential=$(aws configure get aws_access_key_id)/20260101/eu-central-1/bedrock/aws4_request, SignedHeaders=host, Signature=x" \
  -d "{\"arn\": \"$ARN\"}"
```

Rotated STS keys from the same account automatically inherit the registered ARN.
