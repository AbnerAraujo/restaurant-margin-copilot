/**
 * `/close` — "Today's Close", per redesign-spec.md §1/§4.1.
 *
 * STUB: the real page (today's reconciliation summary card + the 14-day
 * margin bar chart) is built by a parallel agent. This placeholder exists
 * only so the shell's routing can be built and tested independently of that
 * work.
 */
export default function ClosePage() {
  return (
    <div className="mx-auto flex max-w-3xl flex-col gap-4">
      <h1 className="text-2xl font-semibold tracking-tight text-foreground">
        Today&apos;s Close
      </h1>
      <p className="text-sm text-muted-foreground">
        Reconciliation summary and the 14-day margin chart go here.
      </p>
    </div>
  )
}
