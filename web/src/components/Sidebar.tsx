import { useState } from "react";
import { NavLink } from "react-router-dom";
import { useWSStatus } from "@/hooks/useSSE";

const navItems = [
  { to: "/", label: "Dashboard", icon: "M3 12l2-2m0 0l7-7 7 7M5 10v10a1 1 0 001 1h3m10-11l2 2m-2-2v10a1 1 0 01-1 1h-3m-4 0a1 1 0 01-1-1v-4a1 1 0 011-1h2a1 1 0 011 1v4a1 1 0 01-1 1" },
  { to: "/activity", label: "Activity", icon: "M9 5H7a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2V7a2 2 0 00-2-2h-2M9 5a2 2 0 002 2h2a2 2 0 002-2M9 5a2 2 0 012-2h2a2 2 0 012 2" },
  { to: "/models", label: "Models", icon: "M19.428 15.428a2 2 0 00-1.022-.547l-2.387-.477a6 6 0 00-3.86.517l-.318.158a6 6 0 01-3.86.517L6.05 15.21a2 2 0 00-1.806.547M8 4h8l-1 1v5.172a2 2 0 00.586 1.414l5 5c1.26 1.26.367 3.414-1.415 3.414H4.828c-1.782 0-2.674-2.154-1.414-3.414l5-5A2 2 0 009 10.172V5L8 4z" },
];

export function Sidebar() {
  const [expanded, setExpanded] = useState(false);
  const sseConnected = useWSStatus();

  return (
    <nav
      className="fixed left-0 top-0 h-screen bg-surface-primary border-r border-border-primary z-50 flex flex-col transition-all duration-300"
      style={{
        width: expanded ? 240 : 64,
        transitionTimingFunction: "cubic-bezier(0, .5, .1, 1)",
      }}
      onMouseEnter={() => setExpanded(true)}
      onMouseLeave={() => setExpanded(false)}
    >
      <div className="h-16 flex items-center px-4 border-b border-border-primary">
        <div className="relative w-8 h-8 flex-shrink-0">
          <svg viewBox="0 0 64 64" fill="none" className="w-8 h-8">
            <path d="M32 4L56 18V46L32 60L8 46V18L32 4Z" stroke="#0057FF" strokeWidth="2.5"/>
            <path d="M32 16L46 32L32 48L18 32L32 16Z" fill="#0057FF"/>
            <line x1="32" y1="4" x2="32" y2="16" stroke="#0057FF" strokeWidth="2.5" strokeLinecap="round"/>
            <line x1="32" y1="48" x2="32" y2="60" stroke="#0057FF" strokeWidth="2.5" strokeLinecap="round"/>
            <line x1="8" y1="32" x2="18" y2="32" stroke="#0057FF" strokeWidth="2.5" strokeLinecap="round"/>
            <line x1="46" y1="32" x2="56" y2="32" stroke="#0057FF" strokeWidth="2.5" strokeLinecap="round"/>
            <circle cx="32" cy="32" r="4" fill="white"/>
          </svg>
          <span
            className={`absolute -top-0.5 -right-0.5 w-2.5 h-2.5 rounded-full border-2 border-surface-primary transition-colors duration-300 ${
              sseConnected ? "bg-status-success" : "bg-status-error"
            }`}
            title={sseConnected ? "Live" : "Disconnected"}
          />
        </div>
        {expanded && (
          <div className="ml-3 overflow-hidden">
            <span className="text-sm font-[680] whitespace-nowrap block">
              Bedrock Proxy
            </span>
            <span
              className={`text-[10px] whitespace-nowrap ${
                sseConnected ? "text-status-success" : "text-status-error"
              }`}
            >
              {sseConnected ? "Live" : "Disconnected"}
            </span>
          </div>
        )}
      </div>

      <div className="flex-1 py-4 flex flex-col gap-1 px-2">
        {navItems.map((item) => (
          <NavLink
            key={item.to}
            to={item.to}
            end={item.to === "/"}
            className={({ isActive }) =>
              `flex items-center gap-3 px-3 py-2.5 rounded-[8px] transition-colors duration-150 ${
                isActive
                  ? "bg-surface-secondary text-content-primary font-[580]"
                  : "text-content-secondary hover:bg-hover-primary"
              }`
            }
          >
            <svg
              className="w-5 h-5 flex-shrink-0"
              fill="none"
              viewBox="0 0 24 24"
              stroke="currentColor"
              strokeWidth={1.5}
            >
              <path strokeLinecap="round" strokeLinejoin="round" d={item.icon} />
            </svg>
            {expanded && (
              <span className="text-sm whitespace-nowrap overflow-hidden">
                {item.label}
              </span>
            )}
          </NavLink>
        ))}
      </div>
    </nav>
  );
}
