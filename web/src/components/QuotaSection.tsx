import { useQuery } from "@tanstack/react-query";
import { api, type QuotaWithUsage } from "@/api/client";
import { formatCost, formatTokens } from "@/lib/format";

function ProgressBar({
  value,
  max,
  label,
}: {
  value: number;
  max: number;
  label: string;
}) {
  if (max <= 0) return null;
  const ratio = Math.min(value / max, 1);
  const pct = ratio * 100;

  let color = "bg-status-success";
  if (ratio >= 0.95) color = "bg-status-error";
  else if (ratio >= 0.8) color = "bg-status-warning";

  return (
    <div className="flex items-center gap-3">
      <span className="text-xs text-content-secondary w-16 shrink-0">
        {label}
      </span>
      <div className="flex-1 h-1.5 rounded-full bg-grey-100 overflow-hidden">
        <div
          className={`h-full rounded-full transition-all duration-300 ${color}`}
          style={{ width: `${pct}%` }}
        />
      </div>
      <span className="text-xs text-content-secondary w-24 text-right shrink-0">
        {label === "Cost"
          ? `${formatCost(value)} / ${formatCost(max)}`
          : label === "Tokens"
            ? `${formatTokens(value)} / ${formatTokens(max)}`
            : `${value} / ${max}`}
      </span>
    </div>
  );
}

function QuotaCard({ quota }: { quota: QuotaWithUsage }) {
  return (
    <div
      className={`rounded-[16px] border border-border-primary bg-surface-elevated p-5 shadow-sm ${
        !quota.enabled ? "opacity-50" : ""
      }`}
    >
      <div className="flex items-start justify-between mb-4">
        <div className="min-w-0">
          <p className="text-sm font-[680] text-content-primary truncate">
            {quota.match}
          </p>
          <div className="flex items-center gap-2 mt-1">
            <span
              className={`inline-block rounded-[4px] px-2 py-0.5 text-xs font-[580] ${
                quota.mode === "reject"
                  ? "bg-status-error/15 text-status-error"
                  : "bg-status-warning/15 text-status-warning"
              }`}
            >
              {quota.mode}
            </span>
            {!quota.enabled && (
              <span className="text-xs text-content-tertiary">disabled</span>
            )}
          </div>
        </div>
      </div>

      <div className="space-y-2">
        {quota.tokens_per_day > 0 && (
          <ProgressBar
            label="Tokens"
            value={quota.tokens_used_today}
            max={quota.tokens_per_day}
          />
        )}
        {quota.cost_per_day > 0 && (
          <ProgressBar
            label="Cost"
            value={quota.cost_used_today}
            max={quota.cost_per_day}
          />
        )}
        {quota.requests_per_minute > 0 && (
          <ProgressBar
            label="Req/min"
            value={quota.requests_last_minute}
            max={quota.requests_per_minute}
          />
        )}
      </div>
    </div>
  );
}

export function QuotaSection() {
  const { data: quotas } = useQuery({
    queryKey: ["quotas"],
    queryFn: () => api.getQuotas(),
  });

  if (!quotas || quotas.length === 0) return null;

  return (
    <div className="mb-8">
      <h2 className="text-sm font-[580] text-content-secondary uppercase tracking-wide mb-3">
        Quotas
      </h2>
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
        {quotas.map((q) => (
          <QuotaCard key={q.id} quota={q} />
        ))}
      </div>
    </div>
  );
}
