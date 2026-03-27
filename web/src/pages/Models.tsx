import { useQuery } from "@tanstack/react-query";
import { api } from "@/api/client";
import { Table } from "@/components/Table";
import { formatCost } from "@/lib/format";
import type { Model } from "@/api/client";

export function Models() {
  const { data: models } = useQuery({
    queryKey: ["models"],
    queryFn: () => api.getModels(),
  });

  return (
    <div>
      <h1 className="text-[28px] font-[680] text-content-primary mb-8">
        Models
      </h1>

      <Table<Model>
        keyFn={(m) => m.id}
        columns={[
          {
            key: "name",
            label: "Model",
            render: (m) => (
              <div>
                <p className="font-[580]">{m.name}</p>
                <p className="text-xs text-content-secondary mt-0.5">{m.id}</p>
              </div>
            ),
          },
          {
            key: "input_price",
            label: "Input / 1M tokens",
            align: "right",
            render: (m) => formatCost(m.input_price_per_million),
          },
          {
            key: "output_price",
            label: "Output / 1M tokens",
            align: "right",
            render: (m) => formatCost(m.output_price_per_million),
          },
          {
            key: "status",
            label: "Status",
            align: "right",
            render: (m) => (
              <span
                className={`inline-block rounded-[4px] px-2 py-0.5 text-xs font-[580] ${
                  m.enabled
                    ? "bg-status-success/15 text-status-success"
                    : "bg-grey-200 text-content-secondary"
                }`}
              >
                {m.enabled ? "Enabled" : "Disabled"}
              </span>
            ),
          },
        ]}
        data={models ?? []}
      />
    </div>
  );
}
