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
import ChangePasswordPage from "./pages/ChangePasswordPage"
import UserManagementPage from "./pages/UserManagementPage"
import WifiHeatmapAdminPage from "./pages/WifiHeatmapAdminPage"

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
    <div className="flex h-screen bg-gray-950 overflow-hidden">
      <AmrSidebar />
      <main className="flex-1 overflow-hidden flex flex-col">
        <Routes>
          <Route path="/" element={<DashboardPage />} />
          <Route path="/logs" element={<LogsPage />} />
          <Route path="/rds-logs" element={<RdsLogsPage />} />
          <Route path="/agent" element={<AgentPage />} />
          <Route path="/agent/fleet" element={<AMRFleetPage />} />
          <Route path="/amr-logs" element={<AMRLogsPage />} />
          <Route path="/amr/discovery" element={<AdminRoute permission="discovery"><AmrHealthApp embedded /></AdminRoute>} />
          <Route path="/amr/heatmap" element={<AdminRoute permission="heatmap"><AmrHealthApp embedded /></AdminRoute>} />
          <Route path="/admin/wifi-heatmap" element={<AdminRoute permission="heatmap"><WifiHeatmapAdminPage /></AdminRoute>} />
          <Route path="/amr/*" element={<AmrHealthApp embedded />} />
          <Route path="/servers" element={<AdminRoute permission="servers"><ServersPage /></AdminRoute>} />
          <Route path="/sync" element={<AdminRoute permission="sync"><SyncPage /></AdminRoute>} />
          <Route path="/change-password" element={<AdminRoute permission="change_password"><ChangePasswordPage /></AdminRoute>} />
          <Route path="/users" element={<AdminRoute permission="users"><UserManagementPage /></AdminRoute>} />
          <Route path="/fleet" element={<Navigate to="/agent/fleet" replace />} />
          <Route path="*" element={<Navigate to="/" replace />} />
        </Routes>
      </main>
    </div>
  )
}

function AdminRoute({ children, permission }: { children: ReactNode; permission: import('./types').AdminPermission }) {
  const auth = useAuth()
  if (!auth.ready) {
    return null
  }
  if (!auth.isAuthenticated) {
    return <Navigate to="/login" replace />
  }
  if (!auth.hasPermission(permission)) return <Navigate to="/" replace />
  return <>{children}</>
}
