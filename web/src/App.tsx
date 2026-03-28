import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { api, TIME_RANGES, type TimeRange } from "./api/client";
import { TimeRangePicker } from "./components/TimeRangePicker";
import { StatCard } from "./components/StatCard";
import { Table } from "./components/Table";
import { StatusBadge } from "./components/StatusBadge";
import { useWS, useWSStatus } from "./hooks/useSSE";
import {
  formatCost,
  formatTokens,
  formatNumber,
  formatLatency,
  timeAgo,
  shortenArn,
} from "./lib/format";
import type { Caller, ActivityEntry } from "./api/client";

export function App() {
  useWS();
  const connected = useWSStatus();
  const [range, setRange] = useState<TimeRange>(TIME_RANGES[2]);

  const { data: summary } = useQuery({
    queryKey: ["usage-summary", range.minutes],
    queryFn: () => api.getUsageSummary(range.minutes),
  });

  const { data: callers } = useQuery({
    queryKey: ["callers", range.minutes],
    queryFn: () => api.getCallers(range.minutes),
  });

  const { data: activity } = useQuery({
    queryKey: ["activity"],
    queryFn: () => api.getActivity(100),
  });

  return (
    <div className="min-h-screen bg-surface-primary">
      {/* Header */}
      <header className="flex items-center justify-between px-8 py-5">
        <div className="flex items-center gap-3">
          <div className="relative">
            <svg viewBox="0 0 64 64" fill="none" className="w-7 h-7">
              <path d="M32 4L56 18V46L32 60L8 46V18L32 4Z" stroke="#000" strokeWidth="4" />
              <path d="M32 16L46 32L32 48L18 32L32 16Z" fill="#000" />
              <line x1="32" y1="4" x2="32" y2="16" stroke="#000" strokeWidth="4" strokeLinecap="round" />
              <line x1="32" y1="48" x2="32" y2="60" stroke="#000" strokeWidth="4" strokeLinecap="round" />
              <line x1="8" y1="32" x2="18" y2="32" stroke="#000" strokeWidth="4" strokeLinecap="round" />
              <line x1="46" y1="32" x2="56" y2="32" stroke="#000" strokeWidth="4" strokeLinecap="round" />
              <circle cx="32" cy="32" r="4" fill="white" />
            </svg>
            <span
              className={`absolute -top-0.5 -right-0.5 w-2 h-2 rounded-full border-[1.5px] border-surface-primary transition-colors duration-300 ${
                connected ? "bg-status-success" : "bg-status-error"
              }`}
            />
          </div>
          <span className="text-lg font-[680] text-content-primary">
            Bedrock Proxy
          </span>
        </div>
        <TimeRangePicker value={range} onChange={setRange} />
      </header>

      <main className="max-w-[1440px] mx-auto px-8 pb-8">
        {/* Stats */}
        <div className="grid grid-cols-2 lg:grid-cols-4 gap-4 mb-8">
          <StatCard
            label="Requests"
            value={summary ? formatNumber(summary.total_requests) : "-"}
            subtitle={`Last ${range.label}`}
          />
          <StatCard
            label="Cost"
            value={summary ? formatCost(summary.total_cost_usd) : "-"}
            subtitle={`Last ${range.label}`}
          />
          <StatCard
            label="Tokens"
            value={
              summary
                ? formatTokens(summary.total_input_tokens + summary.total_output_tokens)
                : "-"
            }
            subtitle="Input + Output"
          />
          <StatCard
            label="Callers"
            value={summary ? formatNumber(summary.unique_callers) : "-"}
            subtitle="Distinct IAM roles"
          />
        </div>

        {/* Callers */}
        {callers && callers.length > 0 && (
          <div className="mb-8">
            <h2 className="text-sm font-[580] text-content-secondary uppercase tracking-wide mb-3">
              Usage per Caller
            </h2>
            <Table<Caller>
              keyFn={(c) => `${c.account_id}:${c.role}`}
              versionFn={(c) => `${c.total_requests}:${c.total_cost_usd}`}
              columns={[
                {
                  key: "account",
                  label: "Account",
                  render: (c) => (
                    <span className="text-sm text-content-secondary font-mono">
                      {c.account_id}
                    </span>
                  ),
                },
                {
                  key: "role",
                  label: "Role",
                  render: (c) => (
                    <span className="font-[580]" title={c.role}>
                      {shortenArn(c.role)}
                    </span>
                  ),
                },
                {
                  key: "requests",
                  label: "Requests",
                  align: "right",
                  render: (c) => formatNumber(c.total_requests),
                },
                {
                  key: "tokens",
                  label: "Tokens",
                  align: "right",
                  render: (c) =>
                    formatTokens(c.total_input_tokens + c.total_output_tokens),
                },
                {
                  key: "cost",
                  label: "Cost",
                  align: "right",
                  render: (c) => (
                    <span className="font-[580]">
                      {formatCost(c.total_cost_usd)}
                    </span>
                  ),
                },
              ]}
              data={callers}
            />
          </div>
        )}

        {/* Activity */}
        <h2 className="text-sm font-[580] text-content-secondary uppercase tracking-wide mb-3">
          Activity
        </h2>
        <Table<ActivityEntry>
          keyFn={(a) => a.id}
          versionFn={(a) => a.id}
          columns={[
            {
              key: "time",
              label: "Time",
              render: (a) => (
                <span className="text-content-secondary text-xs">
                  {timeAgo(a.created_at)}
                </span>
              ),
            },
            {
              key: "caller",
              label: "Caller",
              render: (a) => (
                <span className="font-[580] text-sm" title={a.caller}>
                  {shortenArn(a.caller)}
                </span>
              ),
            },
            {
              key: "model",
              label: "Model",
              render: (a) => (
                <span className="text-sm">
                  {a.model_id.replace("eu.", "").replace("anthropic.", "").replace(/-v\d.*$/, "")}
                </span>
              ),
            },
            {
              key: "tokens",
              label: "Tokens",
              align: "right",
              render: (a) => (
                <span className="text-sm">
                  <span className="text-content-secondary">
                    {formatTokens(a.input_tokens)}
                  </span>
                  <span className="text-content-tertiary mx-0.5">/</span>
                  {formatTokens(a.output_tokens)}
                </span>
              ),
            },
            {
              key: "cost",
              label: "Cost",
              align: "right",
              render: (a) => (
                <span className="text-sm">{formatCost(a.cost_usd)}</span>
              ),
            },
            {
              key: "latency",
              label: "Latency",
              align: "right",
              render: (a) => (
                <span className="text-sm text-content-secondary">
                  {formatLatency(a.latency_ms)}
                </span>
              ),
            },
            {
              key: "status",
              label: "",
              align: "right",
              render: (a) => <StatusBadge code={a.status_code} />,
            },
          ]}
          data={activity ?? []}
        />
      </main>
    </div>
  );
}
