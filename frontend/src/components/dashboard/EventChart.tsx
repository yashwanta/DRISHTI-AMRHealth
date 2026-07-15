import {
  AreaChart, Area, XAxis, YAxis, CartesianGrid,
  Tooltip, Legend, ResponsiveContainer
} from 'recharts'
import { format, parseISO, isValid } from 'date-fns'
import type { TimelinePoint } from '../../types'

interface Props { data: TimelinePoint[] }

const EVENT_COLORS: Record<string, string> = {
  crash:     '#ef4444',
  power_off: '#f97316',
  error:     '#eab308',
  warning:   '#8b5cf6',
}

const EVENT_LABELS: Record<string, string> = {
  crash:     '💥 App Crash',
  power_off: '⚡ Restart',
  error:     '❌ Error',
  warning:   '⚠️ Warning',
}

function safeFormat(hour: string): string {
  try {
    const d = parseISO(hour)
    return isValid(d) ? format(d, 'MM/dd h a') : hour
  } catch { return hour }
}

function buildChartData(raw: TimelinePoint[]) {
  const byHour: Record<string, Record<string, number>> = {}
  for (const p of raw) {
    if (!byHour[p.hour]) byHour[p.hour] = {}
    byHour[p.hour][p.event_type] = (byHour[p.hour][p.event_type] || 0) + p.count
  }
  return Object.entries(byHour)
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([hour, counts]) => ({ hour: safeFormat(hour), ...counts }))
}

export default function EventChart({ data }: Props) {
  const chartData = buildChartData(data)
  const types = [...new Set(data.map(d => d.event_type))]

  if (chartData.length === 0) {
    return (
      <div className="flex items-center justify-center h-48 text-gray-400 text-sm">
        No events in the last 7 days
      </div>
    )
  }

  return (
    <ResponsiveContainer width="100%" height={260}>
      <AreaChart data={chartData} margin={{ top: 5, right: 10, left: -10, bottom: 5 }}>
        <defs>
          {types.map(t => (
            <linearGradient key={t} id={`grad-${t}`} x1="0" y1="0" x2="0" y2="1">
              <stop offset="5%"  stopColor={EVENT_COLORS[t] ?? '#6b7280'} stopOpacity={0.3} />
              <stop offset="95%" stopColor={EVENT_COLORS[t] ?? '#6b7280'} stopOpacity={0} />
            </linearGradient>
          ))}
        </defs>
        <CartesianGrid strokeDasharray="3 3" stroke="#f0f0f0" />
        <XAxis dataKey="hour" tick={{ fontSize: 11 }} tickLine={false} />
        <YAxis tick={{ fontSize: 11 }} tickLine={false} axisLine={false} />
        <Tooltip />
        <Legend formatter={(value) => EVENT_LABELS[value] ?? value} />
        {types.map(t => (
          <Area
            key={t}
            type="monotone"
            dataKey={t}
            name={t}
            stroke={EVENT_COLORS[t] ?? '#6b7280'}
            fill={`url(#grad-${t})`}
            strokeWidth={2}
          />
        ))}
      </AreaChart>
    </ResponsiveContainer>
  )
}
