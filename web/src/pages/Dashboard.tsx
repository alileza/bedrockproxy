import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { api, TIME_RANGES, type TimeRange } from "@/api/client";
import { StatCard } from "@/components/StatCard";
import { Table } from "@/components/Table";
import { TimeRangePicker } from "@/components/TimeRangePicker";
import { formatCost, formatTokens, formatNumber, shortenArn } from "@/lib/format";
import type { Caller } from "@/api/client";

export function Dashboard() {
  const [range, setRange] = useState<TimeRange>(TIME_RANGES[2]); // default 24h

  const { data: summary } = useQuery({
    queryKey: ["usage-summary", range.minutes],
    queryFn: () => api.getUsageSummary(range.minutes),
  });

  const { data: callers } = useQuery({
    queryKey: ["callers", range.minutes],
    queryFn: () => api.getCallers(range.minutes),
  });

  return (
    <div>
      <div className="flex items-center justify-between mb-8">
        <h1 className="text-[28px] font-[680] text-content-primary">
          Dashboard
        </h1>
        <TimeRangePicker value={range} onChange={setRange} />
      </div>

      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4 mb-8">
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
              ? formatTokens(
                  summary.total_input_tokens + summary.total_output_tokens
                )
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

      <h2 className="text-lg font-[680] text-content-primary mb-4">
        Usage per Caller
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
