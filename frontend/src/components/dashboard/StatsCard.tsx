import { clsx } from 'clsx'
import type { LucideIcon } from 'lucide-react'

interface Props {
  label: string
  value: number | string
  Icon: LucideIcon
  color: 'blue' | 'green' | 'red' | 'orange' | 'yellow' | 'purple'
  sub?: string
}

const colorMap = {
  blue:   { bg: 'bg-blue-50',   icon: 'text-blue-600',   border: 'border-blue-100' },
  green:  { bg: 'bg-green-50',  icon: 'text-green-600',  border: 'border-green-100' },
  red:    { bg: 'bg-red-50',    icon: 'text-red-600',    border: 'border-red-100' },
  orange: { bg: 'bg-orange-50', icon: 'text-orange-600', border: 'border-orange-100' },
  yellow: { bg: 'bg-yellow-50', icon: 'text-yellow-600', border: 'border-yellow-100' },
  purple: { bg: 'bg-purple-50', icon: 'text-purple-600', border: 'border-purple-100' },
}

export default function StatsCard({ label, value, Icon, color, sub }: Props) {
  const c = colorMap[color]
  return (
    <div className={clsx('bg-white rounded-xl p-5 border', c.border, 'shadow-sm')}>
      <div className="flex items-center justify-between mb-3">
        <span className="text-sm text-gray-500 font-medium">{label}</span>
        <div className={clsx('p-2 rounded-lg', c.bg)}>
          <Icon size={18} className={c.icon} />
        </div>
      </div>
      <div className="text-3xl font-bold text-gray-900">{value}</div>
      {sub && <div className="text-xs text-gray-400 mt-1">{sub}</div>}
    </div>
  )
}
