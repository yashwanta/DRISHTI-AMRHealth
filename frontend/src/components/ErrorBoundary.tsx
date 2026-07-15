import { Component, type ErrorInfo, type ReactNode } from 'react'
import { AlertTriangle, RefreshCw } from 'lucide-react'

interface Props {
  children: ReactNode
}

interface State {
  error: Error | null
}

/**
 * Catches render errors anywhere in the subtree and shows a themed fallback
 * instead of letting React unmount the whole tree (white screen).
 *
 * Class component because error boundaries require getDerivedStateFromError /
 * componentDidCatch, which only work on classes.
 */
export default class ErrorBoundary extends Component<Props, State> {
  state: State = { error: null }

  static getDerivedStateFromError(error: Error): State {
    return { error }
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    // Surfaces the stack trace to the browser console for debugging.
    console.error('ErrorBoundary caught a render error:', error, info)
  }

  reset = () => this.setState({ error: null })

  render() {
    if (this.state.error) {
      return (
        <div className='flex flex-col items-center justify-center min-h-screen bg-gray-900 text-gray-100 p-6'>
          <div className='max-w-lg w-full bg-gray-800 border border-gray-700 rounded-lg p-6'>
            <div className='flex items-center gap-3 mb-4'>
              <div className='p-2 rounded-lg bg-red-900/40'>
                <AlertTriangle size={22} className='text-red-400' />
              </div>
              <h1 className='text-base font-semibold text-white'>Something went wrong</h1>
            </div>
            <p className='text-sm text-gray-400 mb-4'>
              The page hit an unexpected error while rendering. Try reloading, or return to the dashboard.
            </p>
            <pre className='text-xs text-red-300 bg-gray-900 border border-gray-700 rounded-md p-3 mb-4 overflow-auto max-h-40 font-mono whitespace-pre-wrap break-all'>
              {this.state.error.message}
            </pre>
            <div className='flex gap-2'>
              <button
                onClick={() => window.location.reload()}
                className='inline-flex items-center gap-1.5 text-xs px-3 py-1.5 bg-blue-600 hover:bg-blue-700 text-white rounded-md transition-colors'
              >
                <RefreshCw size={12} />
                Reload page
              </button>
              <button
                onClick={this.reset}
                className='inline-flex items-center gap-1.5 text-xs px-3 py-1.5 bg-gray-700 hover:bg-gray-600 text-gray-200 rounded-md transition-colors'
              >
                Try again
              </button>
              <a
                href='/'
                className='inline-flex items-center text-xs px-3 py-1.5 text-gray-400 hover:text-gray-200 transition-colors'
              >
                Go to Dashboard
              </a>
            </div>
          </div>
        </div>
      )
    }

    return this.props.children
  }
}
