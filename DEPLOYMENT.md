# Deployment

## Docker

```bash
docker run -p 8080:8080 -v $(pwd)/config.yaml:/config.yaml \
  ghcr.io/alileza/bedrockproxy -config /config.yaml
```

Images are published to `ghcr.io/alileza/bedrockproxy` on every push to main (amd64 + arm64).

## IAM policy

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
      "Resource": "arn:aws:s3:::YOUR_BUCKET/bedrockproxy/*"
    }
  ]
}
```

The `S3Flush` statement is only needed if S3 flushing is enabled in config. Remove it if `s3.bucket` is empty.

## Kubernetes

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
      serviceAccountName: bedrockproxy
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
          workingDir: /data
          livenessProbe:
            httpGet:
              path: /api/health
              port: 8080
          readinessProbe:
            httpGet:
              path: /api/health
              port: 8080
          resources:
            requests:
              cpu: 100m
              memory: 64Mi
            limits:
              memory: 256Mi
      volumes:
        - name: config
          configMap:
            name: bedrockproxy-config
        - name: data
          persistentVolumeClaim:
            claimName: bedrockproxy-data
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

The PVC stores `.bedrockproxy-callers.json` and `.bedrockproxy-quotas.json` so caller identity and quota overrides survive pod restarts. A 1Gi `gp3` volume is plenty.

## Prometheus

Scrape `/metrics` on port 8080. Key metrics:

| Metric | Type | Labels |
|---|---|---|
| `bedrockproxy_requests_total` | counter | model, operation, status |
| `bedrockproxy_request_duration_seconds` | histogram | model, operation |
| `bedrockproxy_input_tokens_total` | counter | model, caller |
| `bedrockproxy_output_tokens_total` | counter | model, caller |
| `bedrockproxy_cost_usd_total` | counter | model, caller |
| `bedrockproxy_active_requests` | gauge | |
| `bedrockproxy_quota_exceeded_total` | counter | quota_id, mode, caller |

## Caller identity

The proxy resolves caller identity from the SigV4 Authorization header. For full IAM role ARN display, register once per account:

```bash
ARN=$(aws sts get-caller-identity --query Arn --output text)
curl -X POST http://bedrockproxy:8080/api/register-caller \
  -H "Content-Type: application/json" \
  -H "Authorization: AWS4-HMAC-SHA256 Credential=$(aws configure get aws_access_key_id)/20260101/eu-central-1/bedrock/aws4_request, SignedHeaders=host, Signature=x" \
  -d "{\"arn\": \"$ARN\"}"
```

Rotated STS keys from the same account automatically inherit the registered ARN.

## Health checks

- `GET /api/health` — returns `{"status": "ok"}`
