/**
 * `/promotions` — "Promotion ROI", per redesign-spec.md §1/§4.2.
 *
 * STUB: the real page (4-campaign ROI bar chart, with the unattributable
 * campaign rendered as an explicit refusal state) is built by a parallel
 * agent. This placeholder exists only so the shell's routing can be built
 * and tested independently of that work.
 */
export default function PromotionsPage() {
  return (
    <div className="mx-auto flex max-w-3xl flex-col gap-4">
      <h1 className="text-2xl font-semibold tracking-tight text-foreground">
        Promotion ROI
      </h1>
      <p className="text-sm text-muted-foreground">
        The campaign ROI chart goes here.
      </p>
    </div>
  )
}
