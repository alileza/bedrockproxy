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

## Quota enforcement is per-process (HA with sticky sessions)

Quotas are enforced in the proxy's memory. If you run multiple replicas without sticky sessions, each replica tracks usage independently — a caller could exceed their quota by spreading requests across replicas.

**Recommended**: Use sticky sessions with consistent hashing on the `caller` query parameter. This routes all requests from the same caller to the same replica, making quotas accurate per caller.

```yaml
# Ingress example (nginx)
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  annotations:
    nginx.ingress.kubernetes.io/upstream-hash-by: "$arg_caller"
```

**Graceful rollout**: During deployments, the proxy drains in-flight requests (up to 150s for streaming) and flushes remaining data to S3 before exiting. Configure your deployment strategy accordingly:

```yaml
spec:
  strategy:
    type: RollingUpdate
    rollingUpdate:
      maxUnavailable: 0      # never remove a pod before new one is ready
      maxSurge: 1
  template:
    spec:
      terminationGracePeriodSeconds: 180  # allow drain + S3 flush
```

This applies to both **deployments** and **node rotations** (spot reclaim, OS upgrades, cluster autoscaler). The flow:

1. Pod gets SIGTERM (deploy, node drain, spot eviction)
2. Readiness probe fails → ingress stops routing new requests
3. Pod drains in-flight requests (up to 150s for streaming)
4. Pod flushes remaining data to S3
5. Pod exits

To prevent all replicas being evicted at once during node rotation, add a PodDisruptionBudget:

```yaml
apiVersion: policy/v1
kind: PodDisruptionBudget
metadata:
  name: bedrockproxy
spec:
  minAvailable: 1
  selector:
    matchLabels:
      app: bedrockproxy
```

This ensures at least one pod is always running, even during node drains.
