import { Routes, Route, Navigate } from "react-router-dom";
import { Sidebar } from "./components/Sidebar";
import { Dashboard } from "./pages/Dashboard";
import { Activity } from "./pages/Activity";
import { Models } from "./pages/Models";
import { useWS } from "./hooks/useSSE";

export function App() {
  useWS();

  return (
    <div className="min-h-screen bg-surface-primary">
      <Sidebar />
      <main className="max-w-[1440px] mx-auto" style={{ padding: "0 40px 0 80px" }}>
        <div className="py-8">
          <Routes>
            <Route path="/" element={<Dashboard />} />
            <Route path="/activity" element={<Activity />} />
            <Route path="/models" element={<Models />} />
            <Route path="*" element={<Navigate to="/" replace />} />
          </Routes>
        </div>
      </main>
    </div>
  );
}
