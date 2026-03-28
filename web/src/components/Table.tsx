import { useEffect, useRef } from "react";

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
  /** Optional function to produce a "version" string per row — when it changes, the row flashes */
  versionFn?: (row: T) => string | number;
}

export function Table<T>({ columns, data, keyFn, versionFn }: TableProps<T>) {
  const prevVersions = useRef<Map<string | number, string | number>>(new Map());
  const changedKeys = useRef<Set<string | number>>(new Set());

  // Compute which rows changed
  const currentVersions = new Map<string | number, string | number>();
  if (versionFn) {
    for (const row of data) {
      const k = keyFn(row);
      const v = versionFn(row);
      currentVersions.set(k, v);

      const prevV = prevVersions.current.get(k);
      if (prevV !== undefined && prevV !== v) {
        changedKeys.current.add(k);
      } else if (prevV === undefined && prevVersions.current.size > 0) {
        // New row
        changedKeys.current.add(k);
      }
    }
  }

  useEffect(() => {
    prevVersions.current = currentVersions;
    // Clear changed keys after animation
    const timer = setTimeout(() => {
      changedKeys.current = new Set();
    }, 1500);
    return () => clearTimeout(timer);
  });

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
          {data.map((row) => {
            const k = keyFn(row);
            const isChanged = changedKeys.current.has(k);
            return (
              <tr
                key={`${k}-${isChanged ? currentVersions.get(k) : "stable"}`}
                className={`border-b border-border-primary last:border-0 hover:bg-hover-primary transition-colors duration-150 ${
                  isChanged ? "animate-highlight" : ""
                }`}
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
            );
          })}
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
