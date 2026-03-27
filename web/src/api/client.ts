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

export const api = {
  getUsageSummary: (days = 30) =>
    fetchJSON<UsageSummary>(`/usage/summary?days=${days}`),
  getCallers: (days = 30) =>
    fetchJSON<Caller[]>(`/usage/callers?days=${days}`),
  getActivity: (limit = 50) =>
    fetchJSON<ActivityEntry[]>(`/usage/activity?limit=${limit}`),
  getModels: () => fetchJSON<Model[]>("/models"),
};
