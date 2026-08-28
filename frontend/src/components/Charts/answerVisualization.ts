/**
 * Wire shape of `AskResponse.visualization` (backend
 * `internal/httpapi/visualization.go`). The chart TYPE is decided in Go from
 * which typed MCP tool ran and the shape of its deterministic result — this
 * module only mirrors that decision's payload, and no code on this side ever
 * re-decides the form.
 */
export type AnswerVisualizationKind = 'table' | 'bar' | 'pie'

export interface VisualizationPoint {
  label: string
  /**
   * Dollars, for geometry only — bar height, pie arc. Never printed: the
   * backend already formatted the authoritative rendering into `display`,
   * and re-formatting `value` here would risk showing a figure that differs
   * from the one the reconciliation engine produced.
   */
  value: number
  display: string
  /**
   * The deterministic layer declined to produce a figure for this category
   * (FR-013 unattributable ROI). Must render as an explicit no-figure state,
   * never as a zero-length mark.
   */
  unavailable?: boolean
  reason?: string
}

export interface AnswerVisualization {
  kind: AnswerVisualizationKind
  title: string
  subtitle?: string
  value_label?: string
  source_tool: string
  columns?: string[]
  rows?: string[][]
  points?: VisualizationPoint[]
}

/**
 * Fixed-order categorical hues (see `--chart-*` in index.css, validated with
 * the dataviz palette validator in both modes). Assigned by index and never
 * cycled: past the fourth category the caller folds the tail into a neutral
 * "Other" rather than generating a fifth hue that nobody with a colour-vision
 * deficiency could separate from the first four.
 */
export const CATEGORICAL_FILLS = [
  'var(--chart-1)',
  'var(--chart-2)',
  'var(--chart-3)',
  'var(--chart-4)',
] as const

export const MAX_CATEGORICAL_SERIES = CATEGORICAL_FILLS.length
