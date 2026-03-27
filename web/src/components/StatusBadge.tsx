interface StatusBadgeProps {
  code: number;
}

export function StatusBadge({ code }: StatusBadgeProps) {
  const isOk = code >= 200 && code < 300;
  return (
    <span
      className={`inline-block rounded-[4px] px-2 py-0.5 text-xs font-[580] ${
        isOk
          ? "bg-status-success/15 text-status-success"
          : "bg-status-error/15 text-status-error"
      }`}
    >
      {code}
    </span>
  );
}
