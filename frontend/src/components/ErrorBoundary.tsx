import { Component, type ErrorInfo, type ReactNode } from 'react'
import { AlertTriangle, RotateCcw } from 'lucide-react'

import { postJson } from '@/lib/api'

/**
 * The "retro feed": a real frontend crash gets reported to
 * `POST /api/client-errors` (backend/internal/httpapi/client_errors.go) so
 * it leaves a queryable trace, rather than only ever being known if someone
 * happens to notice and describe it. Built directly from a real incident: a
 * stale, pre-schema-change chat message in localStorage broke rendering,
 * and there was no record of it happening — only a report of it hours
 * later. This component is the fix for that gap, not just a nicer crash
 * screen.
 *
 * A class component on purpose: `componentDidCatch` is the one React API
 * with no hook equivalent — an error boundary cannot be written as a
 * function component.
 */

interface Props {
  children: ReactNode
  /** Included in the report so the feed can tell which surface broke. */
  component: string
}

interface State {
  error: Error | null
}

export default class ErrorBoundary extends Component<Props, State> {
  state: State = { error: null }

  static getDerivedStateFromError(error: Error): State {
    return { error }
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    // Best-effort, fire-and-forget: a failed report must never throw again
    // inside a component that already exists to handle failure.
    postJson('/api/client-errors', {
      message: error.message,
      component: this.props.component,
      stack: `${error.stack ?? ''}\n${info.componentStack ?? ''}`.trim(),
      url: window.location.href,
      user_agent: navigator.userAgent,
    }).catch(() => undefined)
  }

  private reset = () => {
    this.setState({ error: null })
  }

  render() {
    if (!this.state.error) return this.props.children

    return (
      <div
        role="alert"
        className="mx-auto flex max-w-md flex-col items-center gap-3 rounded-lg border border-destructive/25 bg-destructive/10 p-6 text-center"
      >
        <AlertTriangle
          className="size-6 text-destructive-text"
          aria-hidden="true"
        />
        <p className="text-sm font-medium text-foreground">
          Something broke in {this.props.component}.
        </p>
        <p className="text-xs leading-relaxed text-muted-foreground">
          This has been logged. Nothing else on the page is affected —
          resetting this section starts it fresh.
        </p>
        <button
          type="button"
          onClick={this.reset}
          className="mt-1 inline-flex items-center gap-1.5 rounded-md border border-border bg-card px-3 py-1.5 text-xs font-medium text-foreground hover:bg-accent focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50"
        >
          <RotateCcw className="size-3.5" aria-hidden="true" />
          Reset
        </button>
      </div>
    )
  }
}
