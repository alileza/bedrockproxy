import { useState } from "react";
import { NavLink } from "react-router-dom";
import { useWSStatus } from "@/hooks/useSSE";

const easing = "cubic-bezier(0, .5, .1, 1)";

function NavTab({
  to,
  icon,
  label,
  end,
  expanded,
}: {
  to: string;
  icon: React.ReactNode;
  label: string;
  end?: boolean;
  expanded: boolean;
}) {
  return (
    <NavLink
      to={to}
      end={end}
      className={({ isActive }) =>
        `group/tab relative flex items-center h-[44px] rounded-[12px] transition-[background,width] duration-300 active:scale-[0.95] ${
          expanded ? "w-full" : "w-[44px]"
        } ${isActive ? "bg-[#c4cbd13d]" : "hover:bg-[#c4cbd13d]"}`
      }
      style={{ transitionTimingFunction: easing }}
    >
      <div className="flex-shrink-0 flex items-center justify-center w-[44px] h-[44px] text-content-primary">
        {icon}
      </div>
      <div
        className="overflow-hidden whitespace-nowrap transition-[max-width] duration-300"
        style={{
          maxWidth: expanded ? 240 : 0,
          transitionTimingFunction: easing,
        }}
      >
        <span className="text-content-primary text-sm font-[580]">
          {label}
        </span>
      </div>
      {!expanded && (
        <div className="absolute left-full ml-2 px-2.5 py-1.5 rounded-[8px] bg-content-primary text-white text-xs font-[580] whitespace-nowrap opacity-0 pointer-events-none group-hover/tab:opacity-100 transition-opacity z-50">
          {label}
        </div>
      )}
    </NavLink>
  );
}

export function Sidebar() {
  const [expanded, setExpanded] = useState(false);
  const connected = useWSStatus();

  return (
    <nav
      className="group fixed left-0 top-0 bottom-0 z-10 flex flex-col bg-surface-primary transition-[width] duration-300 overflow-visible"
      style={{
        width: expanded ? 240 : 64,
        transitionTimingFunction: easing,
      }}
    >
      {/* Header: Logo + Label + Collapse toggle */}
      <div className="pt-6 px-[10px]">
        <div className="flex items-center">
          <NavLink
            to="/"
            className="flex-shrink-0 flex items-center justify-center w-[44px] h-[44px] no-underline"
          >
            <div className="relative">
              <svg viewBox="0 0 64 64" fill="none" className="w-6 h-6">
                <path d="M32 4L56 18V46L32 60L8 46V18L32 4Z" stroke="#000" strokeWidth="4" />
                <path d="M32 16L46 32L32 48L18 32L32 16Z" fill="#000" />
                <line x1="32" y1="4" x2="32" y2="16" stroke="#000" strokeWidth="4" strokeLinecap="round" />
                <line x1="32" y1="48" x2="32" y2="60" stroke="#000" strokeWidth="4" strokeLinecap="round" />
                <line x1="8" y1="32" x2="18" y2="32" stroke="#000" strokeWidth="4" strokeLinecap="round" />
                <line x1="46" y1="32" x2="56" y2="32" stroke="#000" strokeWidth="4" strokeLinecap="round" />
                <circle cx="32" cy="32" r="4" fill="white" />
              </svg>
              <span
                className={`absolute -top-0.5 -right-0.5 w-2 h-2 rounded-full border-[1.5px] border-surface-primary transition-colors duration-300 ${
                  connected ? "bg-status-success" : "bg-status-error"
                }`}
              />
            </div>
          </NavLink>
          <div
            className="flex-1 overflow-hidden whitespace-nowrap transition-[max-width] duration-300"
            style={{
              maxWidth: expanded ? 200 : 0,
              transitionTimingFunction: easing,
            }}
          >
            <NavLink to="/" className="no-underline">
              <span className="text-content-primary font-[680] text-lg">
                Bedrock Proxy
              </span>
            </NavLink>
          </div>
          <button
            onClick={() => setExpanded(!expanded)}
            className="flex-shrink-0 flex items-center justify-center w-[28px] h-[28px] rounded-[8px] hover:bg-[#c4cbd13d] transition-[opacity,background] duration-300"
            style={{
              opacity: expanded ? 1 : 0,
              pointerEvents: expanded ? "auto" : "none",
              transitionTimingFunction: easing,
            }}
          >
            <svg
              className="w-4 h-4 text-content-secondary"
              fill="none"
              viewBox="0 0 24 24"
              stroke="currentColor"
              strokeWidth={2}
            >
              <path strokeLinecap="round" strokeLinejoin="round" d="M15.75 19.5L8.25 12l7.5-7.5" />
            </svg>
          </button>
        </div>
      </div>

      {/* Divider */}
      <div
        className="my-2 h-px bg-border-primary"
        style={{ width: "calc(100% - 44px)", marginLeft: 22 }}
      />

      {/* Nav items */}
      <div className="flex flex-col flex-grow px-[10px] gap-2">
        <NavTab
          to="/"
          end
          label="Dashboard"
          expanded={expanded}
          icon={
            <svg className="w-5 h-5" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={1.5}>
              <path strokeLinecap="round" strokeLinejoin="round" d="M3.75 6A2.25 2.25 0 016 3.75h2.25A2.25 2.25 0 0110.5 6v2.25a2.25 2.25 0 01-2.25 2.25H6a2.25 2.25 0 01-2.25-2.25V6zM3.75 15.75A2.25 2.25 0 016 13.5h2.25a2.25 2.25 0 012.25 2.25V18a2.25 2.25 0 01-2.25 2.25H6A2.25 2.25 0 013.75 18v-2.25zM13.5 6a2.25 2.25 0 012.25-2.25H18A2.25 2.25 0 0120.25 6v2.25A2.25 2.25 0 0118 10.5h-2.25a2.25 2.25 0 01-2.25-2.25V6zM13.5 15.75a2.25 2.25 0 012.25-2.25H18a2.25 2.25 0 012.25 2.25V18A2.25 2.25 0 0118 20.25h-2.25A2.25 2.25 0 0113.5 18v-2.25z" />
            </svg>
          }
        />
        <NavTab
          to="/activity"
          label="Activity"
          expanded={expanded}
          icon={
            <svg className="w-5 h-5" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={1.5}>
              <path strokeLinecap="round" strokeLinejoin="round" d="M12 6v6h4.5m4.5 0a9 9 0 11-18 0 9 9 0 0118 0z" />
            </svg>
          }
        />
        <NavTab
          to="/models"
          label="Models"
          expanded={expanded}
          icon={
            <svg className="w-5 h-5" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={1.5}>
              <path strokeLinecap="round" strokeLinejoin="round" d="M9.75 3.104v5.714a2.25 2.25 0 01-.659 1.591L5 14.5M9.75 3.104c-.251.023-.501.05-.75.082m.75-.082a24.301 24.301 0 014.5 0m0 0v5.714c0 .597.237 1.17.659 1.591L19.8 15.3M14.25 3.104c.251.023.501.05.75.082M19.8 15.3l-1.57.393A9.065 9.065 0 0112 15a9.065 9.065 0 00-6.23.693L5 14.5m14.8.8l1.402 1.402c1.232 1.232.65 3.318-1.067 3.611A48.309 48.309 0 0112 21c-2.773 0-5.491-.235-8.135-.687-1.718-.293-2.3-2.379-1.067-3.61L5 14.5" />
            </svg>
          }
        />
      </div>

      {/* Expand/Collapse — visible on nav hover */}
      <button
        onClick={() => setExpanded(!expanded)}
        className="absolute right-[-10px] bottom-6 flex items-center justify-center w-5 h-5 rounded-full bg-surface-primary border border-border-primary opacity-0 group-hover:opacity-100 transition-[transform,opacity] duration-300"
        style={{
          transform: expanded ? "rotate(180deg)" : undefined,
          transitionTimingFunction: easing,
        }}
      >
        <svg className="w-3 h-3" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2}>
          <path strokeLinecap="round" strokeLinejoin="round" d="M8.25 4.5l7.5 7.5-7.5 7.5" />
        </svg>
      </button>
    </nav>
  );
}
