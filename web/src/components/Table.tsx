interface Column<T> {
  key: string;
  label: string;
  render: (row: T) => React.ReactNode;
  align?: "left" | "right";
}

interface TableProps<T> {
  columns: Column<T>[];
  data: T[];
  keyFn: (row: T) => string | number;
}

export function Table<T>({ columns, data, keyFn }: TableProps<T>) {
  return (
    <div className="rounded-[16px] border border-border-primary bg-surface-elevated shadow-sm overflow-hidden">
      <table className="w-full">
        <thead>
          <tr className="border-b border-border-primary">
            {columns.map((col) => (
              <th
                key={col.key}
                className={`px-5 py-3 text-xs font-[580] text-content-secondary uppercase tracking-wide ${
                  col.align === "right" ? "text-right" : "text-left"
                }`}
              >
                {col.label}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {data.map((row) => (
            <tr
              key={keyFn(row)}
              className="border-b border-border-primary last:border-0 hover:bg-hover-primary transition-colors duration-150"
            >
              {columns.map((col) => (
                <td
                  key={col.key}
                  className={`px-5 py-3.5 text-sm ${
                    col.align === "right" ? "text-right" : "text-left"
                  }`}
                >
                  {col.render(row)}
                </td>
              ))}
            </tr>
          ))}
          {data.length === 0 && (
            <tr>
              <td
                colSpan={columns.length}
                className="px-5 py-8 text-center text-sm text-content-secondary"
              >
                No data yet
              </td>
            </tr>
          )}
        </tbody>
      </table>
    </div>
  );
}
