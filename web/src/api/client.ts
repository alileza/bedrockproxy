const BASE = "/api";

async function fetchJSON<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`${BASE}${path}`, init);
  if (!res.ok) throw new Error(`${res.status}: ${await res.text()}`);
  return res.json();
}

export interface UsageSummary {
  total_requests: number;
  total_input_tokens: number;
  total_output_tokens: number;
  total_cost_usd: number;
  unique_callers: number;
}

export interface Caller {
  account_id: string;
  role: string;
  total_requests: number;
  total_input_tokens: number;
  total_output_tokens: number;
  total_cost_usd: number;
  quota_id?: string;
  quota_match?: string;
  quota_mode?: string;
  quota_exceeded: boolean;
  quota_reason?: string;
}

export interface ActivityEntry {
  id: number;
  caller: string;
  model_id: string;
  operation: string;
  input_tokens: number;
  output_tokens: number;
  cost_usd: number;
  latency_ms: number;
  status_code: number;
  created_at: string;
}

export interface Model {
  id: string;
  name: string;
  input_price_per_million: number;
  output_price_per_million: number;
  enabled: boolean;
  created_at: string;
}

export type TimeRange = {
  label: string;
  minutes: number;
};

export const TIME_RANGES: TimeRange[] = [
  { label: "15m", minutes: 15 },
  { label: "1h", minutes: 60 },
  { label: "24h", minutes: 1440 },
  { label: "3d", minutes: 4320 },
  { label: "7d", minutes: 10080 },
];

export type QuotaMode = "warn" | "reject";

export interface QuotaWithUsage {
  id: string;
  match: string;
  tokens_per_day: number;
  requests_per_minute: number;
  cost_per_day: number;
  mode: QuotaMode;
  enabled: boolean;
  tokens_used_today: number;
  cost_used_today: number;
  requests_last_minute: number;
}

export interface QuotaInput {
  id: string;
  match: string;
  tokens_per_day: number;
  requests_per_minute: number;
  cost_per_day: number;
  mode: QuotaMode;
  enabled: boolean;
}

export const api = {
  getUsageSummary: (minutes = 43200) =>
    fetchJSON<UsageSummary>(`/usage/summary?minutes=${minutes}`),
  getCallers: (minutes = 43200) =>
    fetchJSON<Caller[]>(`/usage/callers?minutes=${minutes}`),
  getActivity: (limit = 50) =>
    fetchJSON<ActivityEntry[]>(`/usage/activity?limit=${limit}`),
  getModels: () => fetchJSON<Model[]>("/models"),

  getQuotas: () => fetchJSON<QuotaWithUsage[]>("/quotas"),
  setQuota: (q: QuotaInput) =>
    fetchJSON<QuotaInput>("/quotas", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(q),
    }),
  deleteQuota: (id: string) =>
    fetchJSON<{ status: string }>(`/quotas/${encodeURIComponent(id)}`, {
      method: "DELETE",
    }),
};
