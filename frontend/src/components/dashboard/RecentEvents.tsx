import { formatDistanceToNow, parseISO, isValid } from 'date-fns'
import type { LogEvent } from '../../types'
import { clsx } from 'clsx'

interface Props { events: LogEvent[] }

const severityBadge: Record<string, string> = {
  critical: 'bg-red-100 text-red-700',
  high:     'bg-orange-100 text-orange-700',
  medium:   'bg-yellow-100 text-yellow-700',
  low:      'bg-gray-100 text-gray-600',
  info:     'bg-blue-100 text-blue-600',
}

const typeIcon: Record<string, string> = {
  crash:        '💥',
  power_off:    '⚡',
  error:        '❌',
  warning:      '⚠️',
  info:         'ℹ️',
  robot_offline:'🤖🔴',
  robot_online: '🤖🟢',
  disk_error:   '💾',
  update:       '🔄',
}

const typeLabel: Record<string, string> = {
  crash:        'Crash',
  power_off:    'Restart',
  error:        'Error',
  warning:      'Warning',
  info:         'Info',
  robot_offline:'Robot Offline',
  robot_online: 'Robot Online',
  disk_error:   'Disk Error',
  update:       'Update',
}

function safeRelative(ts: string): string {
  try {
    const d = parseISO(ts)
    return isValid(d) ? formatDistanceToNow(d, { addSuffix: true }) : '—'
  } catch { return '—' }
}

function cleanMessage(raw: string): string {
  const iso = raw.match(/^\S+T\S+\s+\S+\s+\S+:\s+(.+)$/s)
  if (iso) return iso[1].trim()
  const syslog = raw.match(/^\w+\s+\d+\s+[\d:]+\s+\S+\s+\S+:\s+(.+)$/s)
  if (syslog) return syslog[1].trim()
  return raw.trim()
}

export default function RecentEvents({ events }: Props) {
  if (events.length === 0) {
    return <div className="text-center text-gray-400 text-sm py-8">No recent events</div>
  }

  return (
    <div className="divide-y divide-gray-50">
      {events.map(ev => (
        <div key={ev.id} className="flex items-start gap-3 py-3">
          <span className="text-base mt-0.5 shrink-0">{typeIcon[ev.event_type] ?? '•'}</span>
          <div className="flex-1 min-w-0">
            <div className="flex items-center gap-2 mb-0.5 flex-wrap">
              <span className="text-xs font-semibold text-gray-600">{ev.server_name}</span>
              <span className="text-xs text-gray-400">{typeLabel[ev.event_type] ?? ev.event_type}</span>
              <span className={clsx('text-xs px-1.5 py-0.5 rounded-full font-medium', severityBadge[ev.severity] ?? severityBadge.low)}>
                {ev.severity}
              </span>
              <span className="text-xs text-gray-400 ml-auto whitespace-nowrap">
                {safeRelative(ev.timestamp)}
              </span>
            </div>
            <p className="text-sm text-gray-700 truncate">{cleanMessage(ev.message)}</p>
          </div>
        </div>
      ))}
    </div>
  )
}
