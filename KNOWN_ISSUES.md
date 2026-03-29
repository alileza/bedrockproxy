# Known Issues

## Caller identity requires manual `?caller=` parameter

The proxy cannot automatically resolve the caller's full IAM role ARN. When a client sends a SigV4-signed request, the proxy receives the temporary access key ID in the `Authorization` header, but:

- `sts:GetAccessKeyInfo` only returns the **account ID**, not the role name
- `sts:GetCallerIdentity` requires the caller's **secret key**, which is never transmitted (it's only used to compute the HMAC signature)
- The `X-Amz-Security-Token` is an opaque blob — its internal format is undocumented

**Workaround**: Callers must include their ARN in the endpoint URL:

```bash
# Get your ARN once
ARN=$(aws sts get-caller-identity --query Arn --output text)

# Use it in the endpoint URL
aws bedrock-runtime converse \
    --endpoint-url "http://bedrockproxy:8080?caller=$ARN" \
    ...
```

For production on EKS with IRSA, set it once per pod:

```bash
export BEDROCK_ENDPOINT_URL="http://bedrockproxy:8080?caller=$(aws sts get-caller-identity --query Arn --output text)"
```

**Why not make it optional?** Without a caller identity, the proxy can't attribute usage or enforce quotas per caller. Rather than showing misleading data (raw access key IDs that rotate every hour), we require explicit identification.

## In-memory store is lost on restart

All request history is cleared when the proxy restarts. The dashboard shows "since last restart" only.

**Workaround**: Enable S3 flushing for long-term storage:

```yaml
s3:
  bucket: "my-bucket"
  prefix: "bedrockproxy"
  flush_interval: "5m"
```

Flushed data can be queried via Athena or ingested into Snowflake/dbt for historical analytics.

**Why not use a database?** The proxy is designed to be zero-dependency — no Postgres, no Redis. Adding persistence would increase operational complexity. The in-memory store handles real-time monitoring; S3 handles long-term analytics.

## Cost tracking may be inaccurate for streaming responses

For streaming operations (`converse-stream`, `invoke-with-response-stream`), token counts are extracted from response headers (`X-Amzn-Bedrock-Input-Token-Count` / `X-Amzn-Bedrock-Output-Token-Count`). If Bedrock doesn't include these headers, the request is recorded with 0 tokens.

**Workaround**: Enable Bedrock model invocation logging for authoritative token counts. The proxy's cost tracking is best-effort for real-time visibility; the AWS billing data (CUR) is the source of truth for actual costs.

## Model pricing auto-discovery depends on AWS Pricing API

The proxy fetches model prices from the AWS Pricing API (`us-east-1`) on startup. This requires `pricing:GetProducts` and `bedrock:ListFoundationModels` permissions. If the API is unavailable or permissions are missing, cost tracking shows $0.

**Workaround**: Override prices manually in `config.yaml`:

```yaml
models:
  - id: "anthropic.claude-sonnet-4-6"
    name: "Claude Sonnet 4.6"
    input_price_per_million: 3.0
    output_price_per_million: 15.0
    enabled: true
```

## Quota enforcement is per-process

Quotas are enforced in the proxy's memory. If you run multiple replicas, each replica tracks usage independently — a caller could exceed their quota by spreading requests across replicas.

**Workaround**: Run a single replica. For multi-replica deployments, use Bedrock's native invocation logging + Snowflake for accurate aggregation, and treat the proxy's quotas as approximate.
