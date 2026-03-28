import { TIME_RANGES, type TimeRange } from "@/api/client";

interface TimeRangePickerProps {
  value: TimeRange;
  onChange: (range: TimeRange) => void;
}

export function TimeRangePicker({ value, onChange }: TimeRangePickerProps) {
  return (
    <div className="inline-flex rounded-[8px] border border-border-primary overflow-hidden">
      {TIME_RANGES.map((range) => (
        <button
          key={range.minutes}
          onClick={() => onChange(range)}
          className={`px-3 py-1.5 text-xs font-[580] transition-colors duration-150 ${
            value.minutes === range.minutes
              ? "bg-grey-900 text-surface-primary"
              : "bg-surface-primary text-content-secondary hover:bg-hover-primary"
          }`}
        >
          {range.label}
        </button>
      ))}
    </div>
  );
}
