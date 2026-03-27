const BASE = "/api";

async function fetchJSON<T>(path: string): Promise<T> {
  const res = await fetch(`${BASE}${path}`);
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
  access_key_id: string;
  display_name: string;
  total_requests: number;
  total_input_tokens: number;
  total_output_tokens: number;
  total_cost_usd: number;
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

export const api = {
  getUsageSummary: (minutes = 43200) =>
    fetchJSON<UsageSummary>(`/usage/summary?minutes=${minutes}`),
  getCallers: (minutes = 43200) =>
    fetchJSON<Caller[]>(`/usage/callers?minutes=${minutes}`),
  getActivity: (limit = 50) =>
    fetchJSON<ActivityEntry[]>(`/usage/activity?limit=${limit}`),
  getModels: () => fetchJSON<Model[]>("/models"),
};
