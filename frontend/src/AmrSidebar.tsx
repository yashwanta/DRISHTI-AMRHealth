import { NavLink } from 'react-router-dom'
import { LayoutDashboard, FileText, Radio, BrainCircuit, Activity, ScrollText, Wifi, Server, RefreshCw, LogIn, LogOut } from 'lucide-react'
import { clsx } from 'clsx'
import { useAuth } from './auth'

const nav = [
  { to: '/',            label: 'Dashboard',     Icon: LayoutDashboard },
  { to: '/logs',        label: 'Logs',          Icon: FileText },
  { to: '/rds-logs',    label: 'RDS Logs',      Icon: Radio },
  { to: '/agent',       label: 'Agent',         Icon: BrainCircuit },
  { to: '/agent/fleet', label: 'AMR Fleet',     Icon: Activity },
  { to: '/amr-logs',    label: 'AMR Logs',      Icon: ScrollText },
  { to: '/amr/',        label: 'WiFi / Maps',   Icon: Wifi },
]

const adminNav = [
  { to: '/servers', label: 'Servers',   Icon: Server },
  { to: '/sync',    label: 'Sync Jobs', Icon: RefreshCw },
]

export function AmrSidebar() {
  const auth = useAuth()
  return (
    <aside className="w-56 flex-shrink-0 bg-gray-900 text-gray-300 flex flex-col">
      <div className="flex items-center gap-2.5 px-5 py-5 border-b border-gray-700">
        <div className="h-10 w-10 rounded-lg bg-gray-950/40 border border-blue-500/30 flex items-center justify-center overflow-hidden">
          <img src="/Drishti_AMRHealth-logo4.png" alt="DRISHTI AMR Health logo" className="h-full w-full object-contain" />
        </div>
        <div>
          <span className="font-bold text-white text-base tracking-wide">DRISHTI</span>
          <p className="text-xs text-gray-500 leading-none mt-0.5">AMR Health</p>
        </div>
      </div>
      <nav className="flex-1 px-3 py-4 space-y-1">
        {nav.map(({ to, label, Icon }) => (
          <NavLink
            key={to}
            to={to}
            end={to === '/' || to === '/agent'}
            className={({ isActive }) =>
              clsx(
                'flex items-center gap-3 px-3 py-2 rounded-lg text-sm font-medium transition-colors',
                isActive ? 'bg-blue-600 text-white' : 'hover:bg-gray-800 hover:text-white'
              )
            }
          >
            <Icon size={18} />
            <span>{label}</span>
          </NavLink>
        ))}
        <div className="pt-4 mt-4 border-t border-gray-800">
          <div className="px-3 pb-2 text-[11px] uppercase tracking-wider text-gray-600">Admin</div>
          {auth.isAuthenticated ? (
            adminNav.map(({ to, label, Icon }) => (
              <NavLink
                key={to}
                to={to}
                className={({ isActive }) =>
                  clsx(
                    'flex items-center gap-3 px-3 py-2 rounded-lg text-sm font-medium transition-colors',
                    isActive ? 'bg-blue-600 text-white' : 'hover:bg-gray-800 hover:text-white'
                  )
                }
              >
                <Icon size={18} />
                <span>{label}</span>
              </NavLink>
            ))
          ) : (
            <NavLink
              to="/login"
              className={({ isActive }) =>
                clsx(
                  'flex items-center gap-3 px-3 py-2 rounded-lg text-sm font-medium transition-colors',
                  isActive ? 'bg-cyan-600 text-white' : 'hover:bg-gray-800 hover:text-white'
                )
              }
            >
              <LogIn size={18} />
              Admin Login
            </NavLink>
          )}
        </div>
      </nav>
      <div className="px-5 py-4 text-xs text-gray-500 border-t border-gray-700">
        {auth.isAuthenticated ? (
          <div className="space-y-2">
            <div>
              <div className="text-gray-400">{auth.username}</div>
              <div className="text-gray-500">{auth.role}</div>
            </div>
            <button
              onClick={() => auth.logout()}
              className="flex items-center gap-2 text-gray-400 hover:text-white transition-colors"
            >
              <LogOut size={14} />
              Sign out
            </button>
          </div>
        ) : (
          <div>Go + React</div>
        )}
      </div>
    </aside>
  )
}