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
  /**
   * What clicking the recovery button actually does. The two boundaries below
   * recover differently — the in-tree one re-renders this section, the router
   * one navigates back to Home — and the shared copy used to describe only
   * the first, while the button said a bare "Reset" that named no outcome at
   * all (ux-writing: frontload the verb, name the outcome).
   */
  recovery: 'section' | 'home'
}

const RECOVERY_COPY = {
  section: {
    explanation: 'Nothing else on the page is affected — resetting this section starts it fresh.',
    button: 'Reset this section',
  },
  home: {
    explanation: 'The rest of the app is unaffected — going back to Home loads it fresh.',
    button: 'Go to Home',
  },
} as const

/**
 * The crash screen itself, shared by both the in-tree `ErrorBoundary` and
 * `RouteErrorBoundary` below, so a crash reads identically to the owner no
 * matter which of the two actually caught it.
 */
function ErrorFallback({ component, onReset, recovery }: FallbackProps) {
  const copy = RECOVERY_COPY[recovery]
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
        This has been logged. {copy.explanation}
      </p>
      <button
        type="button"
        onClick={onReset}
        className="mt-1 inline-flex items-center gap-1.5 rounded-md border border-border bg-card px-3 py-1.5 text-xs font-medium text-foreground hover:bg-accent focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50"
      >
        <RotateCcw className="size-3.5" aria-hidden="true" />
        {copy.button}
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
    return (
      <ErrorFallback
        component={this.props.component}
        onReset={this.reset}
        recovery="section"
      />
    )
  }
}

/**
 * `errorElement` for the router's root route. A failed loader or a
 * malformed route object is caught by React Router itself, before any
 * component subtree exists for a class `ErrorBoundary` to wrap — so this is
 * a separate function component, reading the error via `useRouteError`
 * instead of `componentDidCatch`. Same visual fallback and same
 * `/api/client-errors` report as the in-tree boundary; recovery here is a
 * full navigation back to `/` rather than clearing local component state,
 * since a router-level failure has no children subtree here to re-render.
 * That difference is why the fallback takes a `recovery` prop: this boundary
 * offers "Go to Home", the in-tree one "Reset this section", and neither
 * button promises something the other one's handler would do.
 */
export function RouteErrorBoundary({ component }: { component: string }) {
  const error = useRouteError()

  useEffect(() => {
    reportClientError(component, error instanceof Error ? error : new Error(String(error)))
  }, [component, error])

  return (
    <ErrorFallback
      component={component}
      onReset={() => window.location.assign('/')}
      recovery="home"
    />
  )
}
