import { useQuery } from "@tanstack/react-query";
import { api } from "@/api/client";
import { Table } from "@/components/Table";
import { StatusBadge } from "@/components/StatusBadge";
import { formatCost, formatTokens, formatLatency, timeAgo, shortenArn } from "@/lib/format";
import type { ActivityEntry } from "@/api/client";

export function Activity() {
  const { data: activity } = useQuery({
    queryKey: ["activity"],
    queryFn: () => api.getActivity(100),
  });

  return (
    <div>
      <h1 className="text-[28px] font-[680] text-content-primary mb-8">
        Activity
      </h1>

      <Table<ActivityEntry>
        keyFn={(a) => a.id}
        columns={[
          {
            key: "time",
            label: "Time",
            render: (a) => (
              <span className="text-content-secondary">{timeAgo(a.created_at)}</span>
            ),
          },
          {
            key: "caller",
            label: "Caller",
            render: (a) => (
              <span className="font-[580]" title={a.caller}>
                {shortenArn(a.caller)}
              </span>
            ),
          },
          {
            key: "model",
            label: "Model",
            render: (a) => {
              const short = a.model_id.replace("anthropic.", "").replace(/-v\d.*$/, "");
              return <span className="text-sm">{short}</span>;
            },
          },
          {
            key: "operation",
            label: "Op",
            render: (a) => (
              <span className="text-xs text-content-secondary">{a.operation}</span>
            ),
          },
          {
            key: "tokens",
            label: "Tokens",
            align: "right",
            render: (a) => (
              <span>
                <span className="text-content-secondary">{formatTokens(a.input_tokens)}</span>
                <span className="text-content-tertiary mx-1">/</span>
                <span>{formatTokens(a.output_tokens)}</span>
              </span>
            ),
          },
          {
            key: "cost",
            label: "Cost",
            align: "right",
            render: (a) => formatCost(a.cost_usd),
          },
          {
            key: "latency",
            label: "Latency",
            align: "right",
            render: (a) => (
              <span className="text-content-secondary">{formatLatency(a.latency_ms)}</span>
            ),
          },
          {
            key: "status",
            label: "Status",
            align: "right",
            render: (a) => <StatusBadge code={a.status_code} />,
          },
        ]}
        data={activity ?? []}
      />
    </div>
  );
}
