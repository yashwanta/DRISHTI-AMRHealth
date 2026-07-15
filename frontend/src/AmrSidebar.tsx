import { NavLink } from 'react-router-dom'
import { LayoutDashboard, FileText, Radio, BrainCircuit, Activity, ScrollText, Wifi, Signal } from 'lucide-react'
import { clsx } from 'clsx'

const siteopsNav = [
  { to: '/',            label: 'Dashboard',  Icon: LayoutDashboard },
  { to: '/logs',        label: 'Logs',       Icon: FileText },
  { to: '/rds-logs',    label: 'RDS Logs',   Icon: Radio },
  { to: '/agent',       label: 'Agent',      Icon: BrainCircuit },
  { to: '/agent/fleet', label: 'AMR Fleet',  Icon: Activity },
  { to: '/amr-logs',    label: 'AMR Logs',   Icon: ScrollText },
]

const amrNav = [
  { to: '/amr/', label: 'WiFi Health',   Icon: Wifi },
  { to: '/amr/', label: 'Heatmap',       Icon: Signal },
]

export function AmrSidebar() {
  return (
    <aside className="w-56 flex-shrink-0 bg-gray-900 text-gray-300 flex flex-col">
      <div className="flex items-center gap-2.5 px-5 py-5 border-b border-gray-700">
        <div className="h-8 w-8 rounded-lg bg-blue-600/20 border border-blue-500/40 flex items-center justify-center">
          <span className="text-blue-300 font-bold text-sm">D</span>
        </div>
        <div>
          <span className="font-bold text-white text-base tracking-wide">DRISHTI</span>
          <p className="text-xs text-gray-500 leading-none mt-0.5">AMR Health</p>
        </div>
      </div>
      <nav className="flex-1 px-3 py-4 space-y-1">
        <div className="px-3 pb-2 text-[11px] uppercase tracking-wider text-gray-600">Operations</div>
        {siteopsNav.map(({ to, label, Icon }) => (
          <NavLink
            key={to + label}
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
          <div className="px-3 pb-2 text-[11px] uppercase tracking-wider text-gray-600">WiFi / Maps</div>
          <NavLink
            to="/amr/"
            className={({ isActive }) =>
              clsx(
                'flex items-center gap-3 px-3 py-2 rounded-lg text-sm font-medium transition-colors',
                isActive ? 'bg-blue-600 text-white' : 'hover:bg-gray-800 hover:text-white'
              )
            }
          >
            <Wifi size={18} />
            AMR Health Suite
          </NavLink>
        </div>
      </nav>
      <div className="px-5 py-4 text-xs text-gray-500 border-t border-gray-700">
        <div>Go + React</div>
        <div className="text-gray-600">Local RDS proxy enabled</div>
      </div>
    </aside>
  )
}