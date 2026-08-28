import CategoryBarChart from './CategoryBarChart'
import CompositionPieChart from './CompositionPieChart'
import DataGrid from './DataGrid'
import type { AnswerVisualization } from './answerVisualization'

export interface AnswerVisualizationViewProps {
  visualization: AnswerVisualization
  className?: string
}

/**
 * Renders whichever form the BACKEND already chose for an answer. This
 * component dispatches on `kind` and nothing else — it never inspects the
 * data to second-guess the form, because that decision is deterministic Go
 * (`internal/httpapi/visualization.go`) and duplicating it here would create
 * two sources of truth that could disagree about the same answer.
 *
 * An unrecognized `kind` renders nothing rather than falling back to some
 * default chart: a newer backend shape drawn with an older client's guess
 * would be a fabricated rendering of a real number.
 */
export default function AnswerVisualizationView({
  visualization,
  className,
}: AnswerVisualizationViewProps) {
  const { kind, title, subtitle, value_label, source_tool } = visualization

  if (kind === 'table' && visualization.columns && visualization.rows) {
    return (
      <DataGrid
        title={title}
        subtitle={subtitle}
        columns={visualization.columns}
        rows={visualization.rows}
        sourceTool={source_tool}
        className={className}
      />
    )
  }

  if (kind === 'bar' && visualization.points?.length) {
    return (
      <CategoryBarChart
        title={title}
        subtitle={subtitle}
        valueLabel={value_label}
        points={visualization.points}
        sourceTool={source_tool}
        className={className}
      />
    )
  }

  if (kind === 'pie' && visualization.points?.length) {
    return (
      <CompositionPieChart
        title={title}
        subtitle={subtitle}
        valueLabel={value_label}
        points={visualization.points}
        sourceTool={source_tool}
        className={className}
      />
    )
  }

  return null
}
