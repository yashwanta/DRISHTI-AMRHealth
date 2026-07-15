import { BrowserRouter, Navigate, Routes, Route } from "react-router-dom"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import AmrHealthApp from "./AmrHealthApp"
import ErrorBoundary from "./components/ErrorBoundary"
import { AmrSidebar } from "./AmrSidebar"
import DashboardPage from "./pages/DashboardPage"
import LogsPage from "./pages/LogsPage"
import RdsLogsPage from "./pages/RdsLogsPage"
import AgentPage from "./pages/AgentPage"
import AMRFleetPage from "./pages/AMRFleetPage"
import AMRLogsPage from "./pages/AMRLogsPage"

const qc = new QueryClient({
  defaultOptions: { queries: { retry: 1 } },
})

export default function App() {
  return (
    <QueryClientProvider client={qc}>
      <ErrorBoundary>
        <BrowserRouter>
          <Routes>
            <Route path="/amr/*" element={<AmrHealthApp />} />
            <Route path="/*" element={<SiteOpsShell />} />
          </Routes>
        </BrowserRouter>
      </ErrorBoundary>
    </QueryClientProvider>
  )
}

function SiteOpsShell() {
  return (
    <div className="flex h-screen bg-gray-50 overflow-hidden">
      <AmrSidebar />
      <main className="flex-1 overflow-hidden flex flex-col">
        <Routes>
          <Route path="/" element={<DashboardPage />} />
          <Route path="/logs" element={<LogsPage />} />
          <Route path="/rds-logs" element={<RdsLogsPage />} />
          <Route path="/agent" element={<AgentPage />} />
          <Route path="/agent/fleet" element={<AMRFleetPage />} />
          <Route path="/amr-logs" element={<AMRLogsPage />} />
          <Route path="/fleet" element={<Navigate to="/agent/fleet" replace />} />
          <Route path="/amr" element={<Navigate to="/amr/" replace />} />
          <Route path="*" element={<Navigate to="/" replace />} />
        </Routes>
      </main>
    </div>
  )
}