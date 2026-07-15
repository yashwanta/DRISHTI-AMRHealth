import { RefreshCw } from 'lucide-react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { syncAll } from '../../api/client'

interface Props { title: string }

export default function Header({ title }: Props) {
  const qc = useQueryClient()
  const { mutate, isPending } = useMutation({
    mutationFn: () => syncAll(),
    onSuccess: () => {
      qc.invalidateQueries()
    },
  })

  return (
    <header className="h-14 bg-white border-b border-gray-200 flex items-center justify-between px-6">
      <h1 className="text-gray-800 font-semibold text-lg">{title}</h1>
      <button
        onClick={() => mutate()}
        disabled={isPending}
        className="flex items-center gap-2 text-sm bg-blue-600 hover:bg-blue-700 text-white px-4 py-2 rounded-lg disabled:opacity-60 transition-colors"
      >
        <RefreshCw size={15} className={isPending ? 'animate-spin' : ''} />
        {isPending ? 'Syncing…' : 'Sync All'}
      </button>
    </header>
  )
}
