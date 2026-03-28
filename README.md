<p><img src="web/public/logo.svg" width="48" align="left" style="margin-right: 12px" /> <strong style="font-size: 2em">bedrockproxy</strong></p>

<br clear="left" />

A thin proxy in front of AWS Bedrock that tracks who's using what, how much it costs, and shows it all in a real-time dashboard.

**Problem**: AWS Bedrock has no per-IAM-role usage analytics. CloudWatch metrics only break down by model, not by caller. You can't answer "how much did team X spend on Claude this week?"

**Solution**: Point your AWS SDK at bedrockproxy instead of Bedrock. Same auth, same API. The proxy forwards everything to Bedrock and tracks usage per caller.

![Dashboard](docs/dashboard.png)

## Quick start

```bash
# Start the proxy
make dev

# Use it — just add --endpoint-url
aws bedrock-runtime converse \
    --endpoint-url http://localhost:8080 \
    --model-id eu.anthropic.claude-sonnet-4-6 \
    --messages '[{"role":"user","content":[{"text":"Hello"}]}]' \
    --region eu-central-1
```

No new API keys. No new auth. Just change the endpoint URL. Works with Claude Code, Cline, LangChain, boto3 — anything that talks to Bedrock.

## What you get

- **Real-time dashboard** — requests, tokens, cost per caller, updated live via WebSocket
- **All Bedrock operations** — Converse, InvokeModel, streaming variants, CountTokens, guardrails, async invoke
- **Per-caller quotas** — token budgets, cost caps, request rate limits with warn or reject mode
- **Prometheus metrics** — `bedrockproxy_requests_total`, `_cost_usd_total`, `_active_requests`, etc.
- **Auto-discovered pricing** — fetches model prices from AWS on startup, no manual config needed
- **S3 export** — periodic flush of usage data for long-term analytics in Snowflake/Athena

## Why not LiteLLM / Bifrost / etc?

|  | bedrockproxy | LiteLLM / others |
|---|---|---|
| **Migration cost** | Change one line: `endpoint_url` | New SDK integration, new API keys, new auth flow per service |
| **Operational cost** | Single binary, ~20MB RAM, no database | Postgres + Redis + Python runtime |
| **Infrastructure** | One pod on EKS, that's it | Database provisioning, connection pooling, migrations, backups |
| **Auth model** | Your existing IAM roles, unchanged | Issue and manage virtual API keys per team |
| **Identity tracking** | Real AWS IAM role ARNs | Virtual key labels |
| **Time to value** | Deploy → see data in minutes | Weeks of integration across services |

**Use bedrockproxy** if you're on AWS Bedrock and want visibility into who's calling what without touching any client code.

**Use LiteLLM** if you need multi-provider routing, budget enforcement, or OpenAI-compatible API translation.

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

Single binary, no database, ~16MB. In-memory store for real-time dashboard, optional S3 flush for long-term analytics.

## Configuration

```yaml
server:
  port: 8080

aws:
  region: "eu-central-1"

s3:
  bucket: ""          # leave empty to disable
  prefix: "bedrockproxy"
  flush_interval: "5m"

quotas:
  - id: "staging"
    match: "account:590211171383"
    cost_per_day: 10.0
    tokens_per_day: 1000000
    requests_per_minute: 60
    mode: "warn"        # warn | reject
    enabled: true
```

Quota match patterns: `account:<id>`, `arn:aws:sts::*:assumed-role/MyRole*` (glob), or `*` (catch-all).

## Docs

- **[DEPLOYMENT.md](DEPLOYMENT.md)** — EKS deployment, IAM policy, Kubernetes manifests, Prometheus scraping
- **[CONTRIBUTING.md](CONTRIBUTING.md)** — Development setup, project structure, build commands, testing
