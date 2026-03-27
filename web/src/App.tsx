import { Routes, Route, Navigate } from "react-router-dom";
import { Sidebar } from "./components/Sidebar";
import { Dashboard } from "./pages/Dashboard";
import { Activity } from "./pages/Activity";
import { Models } from "./pages/Models";
import { useWS } from "./hooks/useSSE";

export function App() {
  useWS();

  return (
    <div className="flex min-h-screen">
      <Sidebar />
      <main className="flex-1 pl-20 pr-10 py-8 max-w-[1440px]">
        <Routes>
          <Route path="/" element={<Dashboard />} />
          <Route path="/activity" element={<Activity />} />
          <Route path="/models" element={<Models />} />
          <Route path="*" element={<Navigate to="/" replace />} />
        </Routes>
      </main>
    </div>
  );
}
