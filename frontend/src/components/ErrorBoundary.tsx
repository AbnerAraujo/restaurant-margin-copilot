import { Component, type ErrorInfo, type ReactNode, useEffect } from 'react'
import { AlertTriangle, RotateCcw } from 'lucide-react'
import { useRouteError } from 'react-router-dom'

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
 * function component. `RouteErrorBoundary` below covers the one case a
 * class boundary can't: a router-level failure (a failed loader, a
 * malformed route object) that never reaches a subtree for this class to
 * wrap in the first place.
 */

interface Props {
  children: ReactNode
  /** Included in the report so the feed can tell which surface broke. */
  component: string
  /**
   * Runs before the boundary clears its own error state. Exists because
   * `reset()` re-rendering the identical children is not "starting fresh"
   * when the crash was caused by bad persisted data (exactly the incident
   * this component was built from) — without this, clicking Reset re-reads
   * the same poisoned `localStorage` key and crashes again immediately,
   * contradicting the UI copy's own "starts it fresh" promise. Optional
   * because most boundaries wrap pages with no such recoverable state to
   * clear.
   */
  onReset?: () => void
}

interface State {
  error: Error | null
}

function reportClientError(component: string, error: Error, componentStack?: string) {
  // Best-effort, fire-and-forget: a failed report must never throw again
  // inside code that already exists to handle failure.
  postJson('/api/client-errors', {
    message: error.message,
    component,
    stack: `${error.stack ?? ''}\n${componentStack ?? ''}`.trim(),
    url: window.location.href,
    user_agent: navigator.userAgent,
  }).catch(() => undefined)
}

interface FallbackProps {
  component: string
  onReset: () => void
}

/**
 * The crash screen itself, shared by both the in-tree `ErrorBoundary` and
 * `RouteErrorBoundary` below, so a crash reads identically to the owner no
 * matter which of the two actually caught it.
 */
function ErrorFallback({ component, onReset }: FallbackProps) {
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
        Something broke in {component}.
      </p>
      <p className="text-xs leading-relaxed text-muted-foreground">
        This has been logged. Nothing else on the page is affected —
        resetting this section starts it fresh.
      </p>
      <button
        type="button"
        onClick={onReset}
        className="mt-1 inline-flex items-center gap-1.5 rounded-md border border-border bg-card px-3 py-1.5 text-xs font-medium text-foreground hover:bg-accent focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50"
      >
        <RotateCcw className="size-3.5" aria-hidden="true" />
        Reset
      </button>
    </div>
  )
}

export default class ErrorBoundary extends Component<Props, State> {
  state: State = { error: null }

  static getDerivedStateFromError(error: Error): State {
    return { error }
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    reportClientError(this.props.component, error, info.componentStack ?? undefined)
  }

  private reset = () => {
    this.props.onReset?.()
    this.setState({ error: null })
  }

  render() {
    if (!this.state.error) return this.props.children
    return <ErrorFallback component={this.props.component} onReset={this.reset} />
  }
}

/**
 * `errorElement` for the router's root route. A failed loader or a
 * malformed route object is caught by React Router itself, before any
 * component subtree exists for a class `ErrorBoundary` to wrap — so this is
 * a separate function component, reading the error via `useRouteError`
 * instead of `componentDidCatch`. Same visual fallback and same
 * `/api/client-errors` report as the in-tree boundary; "Reset" does a full
 * navigation back to `/` rather than clearing local component state, since
 * a router-level failure has no children subtree here to re-render.
 */
export function RouteErrorBoundary({ component }: { component: string }) {
  const error = useRouteError()

  useEffect(() => {
    reportClientError(component, error instanceof Error ? error : new Error(String(error)))
  }, [component, error])

  return (
    <ErrorFallback component={component} onReset={() => window.location.assign('/')} />
  )
}
