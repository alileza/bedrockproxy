-- Callers: resolved IAM identities
CREATE TABLE IF NOT EXISTS callers (
    id              BIGSERIAL PRIMARY KEY,
    access_key_id   TEXT NOT NULL,
    account_id      TEXT,
    role_arn        TEXT,
    display_name    TEXT,
    first_seen_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (access_key_id)
);

-- Models: Bedrock model pricing config
CREATE TABLE IF NOT EXISTS models (
    id                      TEXT PRIMARY KEY,
    name                    TEXT NOT NULL,
    input_price_per_million  NUMERIC(10, 4) NOT NULL DEFAULT 0,
    output_price_per_million NUMERIC(10, 4) NOT NULL DEFAULT 0,
    enabled                 BOOLEAN NOT NULL DEFAULT TRUE,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Requests: every proxied Bedrock call
CREATE TABLE IF NOT EXISTS requests (
    id              BIGSERIAL PRIMARY KEY,
    caller_id       BIGINT NOT NULL REFERENCES callers(id),
    model_id        TEXT NOT NULL,
    operation       TEXT NOT NULL,
    input_tokens    INT NOT NULL DEFAULT 0,
    output_tokens   INT NOT NULL DEFAULT 0,
    cost_usd        NUMERIC(12, 6) NOT NULL DEFAULT 0,
    latency_ms      INT NOT NULL DEFAULT 0,
    status_code     INT NOT NULL DEFAULT 200,
    error_message   TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_requests_caller_id ON requests(caller_id);
CREATE INDEX IF NOT EXISTS idx_requests_model_id ON requests(model_id);
CREATE INDEX IF NOT EXISTS idx_requests_created_at ON requests(created_at);

-- Daily usage aggregation (materialized by background job)
CREATE TABLE IF NOT EXISTS daily_usage (
    caller_id       BIGINT NOT NULL REFERENCES callers(id),
    model_id        TEXT NOT NULL,
    day             DATE NOT NULL,
    request_count   INT NOT NULL DEFAULT 0,
    input_tokens    BIGINT NOT NULL DEFAULT 0,
    output_tokens   BIGINT NOT NULL DEFAULT 0,
    cost_usd        NUMERIC(12, 6) NOT NULL DEFAULT 0,
    PRIMARY KEY (caller_id, model_id, day)
);
