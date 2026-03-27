import { useQuery } from "@tanstack/react-query";
import { api } from "@/api/client";
import { StatCard } from "@/components/StatCard";
import { Table } from "@/components/Table";
import { formatCost, formatTokens, formatNumber, shortenArn } from "@/lib/format";
import type { Caller } from "@/api/client";

export function Dashboard() {
  const { data: summary } = useQuery({
    queryKey: ["usage-summary"],
    queryFn: () => api.getUsageSummary(30),
  });

  const { data: callers } = useQuery({
    queryKey: ["callers"],
    queryFn: () => api.getCallers(30),
  });

  return (
    <div>
      <h1 className="text-[28px] font-[680] text-content-primary mb-8">
        Dashboard
      </h1>

      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4 mb-8">
        <StatCard
          label="Total Requests"
          value={summary ? formatNumber(summary.total_requests) : "-"}
          subtitle="Last 30 days"
        />
        <StatCard
          label="Total Cost"
          value={summary ? formatCost(summary.total_cost_usd) : "-"}
          subtitle="Last 30 days"
        />
        <StatCard
          label="Total Tokens"
          value={
            summary
              ? formatTokens(
                  summary.total_input_tokens + summary.total_output_tokens
                )
              : "-"
          }
          subtitle="Input + Output"
        />
        <StatCard
          label="Unique Callers"
          value={summary ? formatNumber(summary.unique_callers) : "-"}
          subtitle="Distinct IAM roles"
        />
      </div>

      <h2 className="text-lg font-[680] text-content-primary mb-4">
        Top Callers
      </h2>
      <Table<Caller>
        keyFn={(c) => c.access_key_id}
        columns={[
          {
            key: "caller",
            label: "Caller",
            render: (c) => (
              <span className="font-[580]" title={c.display_name}>
                {shortenArn(c.display_name)}
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
            key: "input",
            label: "Input Tokens",
            align: "right",
            render: (c) => formatTokens(c.total_input_tokens),
          },
          {
            key: "output",
            label: "Output Tokens",
            align: "right",
            render: (c) => formatTokens(c.total_output_tokens),
          },
          {
            key: "cost",
            label: "Cost",
            align: "right",
            render: (c) => (
              <span className="font-[580]">{formatCost(c.total_cost_usd)}</span>
            ),
          },
        ]}
        data={callers ?? []}
      />
    </div>
  );
}
