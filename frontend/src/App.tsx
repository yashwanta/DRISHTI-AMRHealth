import { BrowserRouter, Navigate, Routes, Route } from "react-router-dom"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import type { ReactNode } from "react"
import AmrHealthApp from "./AmrHealthApp"
import ErrorBoundary from "./components/ErrorBoundary"
import { AmrSidebar } from "./AmrSidebar"
import { AuthProvider, useAuth } from "./auth"
import DashboardPage from "./pages/DashboardPage"
import LogsPage from "./pages/LogsPage"
import RdsLogsPage from "./pages/RdsLogsPage"
import AgentPage from "./pages/AgentPage"
import AMRFleetPage from "./pages/AMRFleetPage"
import AMRLogsPage from "./pages/AMRLogsPage"
import LoginPage from "./pages/LoginPage"
import ServersPage from "./pages/ServersPage"
import SyncPage from "./pages/SyncPage"

const qc = new QueryClient({
  defaultOptions: { queries: { retry: 1 } },
})

export default function App() {
  return (
    <QueryClientProvider client={qc}>
      <ErrorBoundary>
        <AuthProvider>
          <BrowserRouter>
            <Routes>
              <Route path="/amr/*" element={<AmrHealthApp />} />
              <Route path="/login" element={<LoginPage />} />
              <Route path="/*" element={<SiteOpsShell />} />
            </Routes>
          </BrowserRouter>
        </AuthProvider>
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
          <Route path="/servers" element={<AdminRoute><ServersPage /></AdminRoute>} />
          <Route path="/sync" element={<AdminRoute><SyncPage /></AdminRoute>} />
          <Route path="/fleet" element={<Navigate to="/agent/fleet" replace />} />
          <Route path="/amr" element={<Navigate to="/amr/" replace />} />
          <Route path="*" element={<Navigate to="/" replace />} />
        </Routes>
      </main>
    </div>
  )
}

function AdminRoute({ children }: { children: ReactNode }) {
  const auth = useAuth()
  if (!auth.isAuthenticated) {
    return <Navigate to="/login" replace />
  }
  return <>{children}</>
}