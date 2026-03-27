interface StatCardProps {
  label: string;
  value: string;
  subtitle?: string;
}

export function StatCard({ label, value, subtitle }: StatCardProps) {
  return (
    <div className="rounded-[16px] border border-border-primary bg-surface-elevated p-5 shadow-sm">
      <p className="text-xs text-content-secondary font-[580] uppercase tracking-wide">
        {label}
      </p>
      <p className="mt-2 text-[28px] font-[680] text-content-primary leading-tight">
        {value}
      </p>
      {subtitle && (
        <p className="mt-1 text-sm text-content-secondary">{subtitle}</p>
      )}
    </div>
  );
}
