import { useQuery } from '@tanstack/react-query'
import { format, parseISO, formatDistanceToNow } from 'date-fns'
import { getSyncHistory } from '../api/client'
import { CheckCircle, XCircle, Loader, Clock } from 'lucide-react'

function duration(start: string, end: string | null): string {
  if (!end) return '—'
  const ms = new Date(end).getTime() - new Date(start).getTime()
  if (ms < 1000) return `${ms}ms`
  return `${(ms / 1000).toFixed(1)}s`
}

export default function SyncPage() {
  const { data: jobs = [] } = useQuery({ queryKey: ['sync-history'], queryFn: getSyncHistory, refetchInterval: 10_000 })

  const statusConfig: Record<string, { icon: React.ReactNode; badge: string }> = {
    running: { icon: <Loader size={13} className="animate-spin text-blue-400" />,  badge: 'bg-blue-900/50 text-blue-300 border border-blue-700' },
    success: { icon: <CheckCircle size={13} className="text-green-400" />,          badge: 'bg-green-900/50 text-green-300 border border-green-700' },
    failed:  { icon: <XCircle size={13} className="text-red-400" />,               badge: 'bg-red-900/50 text-red-300 border border-red-700' },
  }

  return (
    <div className="flex flex-col h-full bg-gray-900 text-gray-100">
      <div className="px-6 py-4 bg-gray-900 border-b border-gray-700">
        <h1 className="text-base font-semibold text-white">Sync Jobs</h1>
        <p className="text-xs text-gray-400 mt-0.5">Automatic syncs run at 6 AM and 6 PM · {jobs.length} recent jobs</p>
      </div>

      <div className="flex-1 overflow-y-auto p-5">
        <div className="bg-gray-800 border border-gray-700 rounded-xl overflow-hidden">
          <table className="w-full text-sm">
            <thead>
              <tr className="text-left text-xs text-gray-400 uppercase tracking-wider border-b border-gray-700 bg-gray-900/50">
                <th className="px-5 py-3 font-medium">Server</th>
                <th className="px-5 py-3 font-medium">Started</th>
                <th className="px-5 py-3 font-medium">Duration</th>
                <th className="px-5 py-3 font-medium">Status</th>
                <th className="px-5 py-3 font-medium">Events found</th>
                <th className="px-5 py-3 font-medium">Error</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-gray-700/50">
              {jobs.length === 0 && (
                <tr><td colSpan={6} className="px-5 py-12 text-center text-gray-500">No sync jobs yet — click Sync All on the dashboard.</td></tr>
              )}
              {jobs.map(j => {
                const cfg = statusConfig[j.status] ?? statusConfig.failed
                return (
                  <tr key={j.id} className="hover:bg-gray-700/30 transition-colors">
                    <td className="px-5 py-3 font-medium text-gray-200">{j.server_name}</td>
                    <td className="px-5 py-3 text-gray-400 whitespace-nowrap">
                      <div className="flex items-center gap-1.5">
                        <Clock size={12} className="text-gray-500" />
                        {format(parseISO(j.started_at), 'MMM d, h:mm a')}
                      </div>
                      <div className="text-xs text-gray-500 mt-0.5">{formatDistanceToNow(parseISO(j.started_at), { addSuffix: true })}</div>
                    </td>
                    <td className="px-5 py-3 text-gray-400 font-mono text-xs">{duration(j.started_at, j.finished_at)}</td>
                    <td className="px-5 py-3">
                      <span className={`inline-flex items-center gap-1.5 text-xs px-2.5 py-1 rounded-full font-medium ${cfg.badge}`}>
                        {cfg.icon} {j.status}
                      </span>
                    </td>
                    <td className="px-5 py-3">
                      {j.event_count > 0
                        ? <span className="text-indigo-400 font-semibold">{j.event_count}</span>
                        : <span className="text-gray-500">0</span>}
                    </td>
                    <td className="px-5 py-3 text-red-400 text-xs max-w-48 truncate" title={j.error}>{j.error || '—'}</td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  )
}
