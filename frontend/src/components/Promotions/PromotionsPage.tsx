import PromoRoiChart from '@/components/Charts/PromoRoiChart'

/**
 * `/promotions` — "Promotion ROI": the 4-campaign ROI bar chart, per
 * redesign-spec.md §1/§4.2. `IFOOD-CAMP-WEEKEND` renders as an explicit
 * unattributable/refused state (FR-013), never a $0 bar; every campaign's
 * provenance stays reachable via its `ProvenanceTag`.
 */
export default function PromotionsPage() {
  return (
    <div className="mx-auto flex max-w-3xl flex-col gap-4">
      <h1 className="text-2xl font-semibold tracking-tight text-foreground">
        Promotion ROI
      </h1>

      <PromoRoiChart />
    </div>
  )
}
