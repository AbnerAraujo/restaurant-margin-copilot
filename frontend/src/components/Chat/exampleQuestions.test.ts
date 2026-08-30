import { describe, expect, it } from 'vitest'

import { MCP_TOOL_NAMES } from '@/capabilities'

import { EXAMPLE_QUESTIONS, buildCapabilitySummary } from './exampleQuestions'

/**
 * A drift alarm for the exact staleness class this file has already shipped
 * once: `EXAMPLE_QUESTIONS` was missing its entry for the 8th MCP tool
 * (`get_expense_pattern_by_day_of_month`) for an entire release, silently,
 * because nothing checked it against the real tool set — the same failure
 * mode `capabilities.test.ts` exists to alarm on for the Help page and the
 * guided composer. This file keeps its own hand-authored list (a content
 * decision — one illustrative question per capability — deliberately not
 * derived from `@/capabilities`, per CHANGELOG.md's "known gap" entry), so it
 * needs its own alarm rather than inheriting one. Ship a 9th tool without
 * adding an example here and this test goes red naming it.
 */
describe('EXAMPLE_QUESTIONS vs. the real MCP tool set', () => {
  it('has exactly one example question per registered MCP tool', () => {
    const tools = EXAMPLE_QUESTIONS.map((q) => q.tool).sort()
    expect(tools).toEqual([...MCP_TOOL_NAMES].sort())
  })

  it('never leaves a raw tool name or empty topic in owner-facing copy', () => {
    for (const question of EXAMPLE_QUESTIONS) {
      expect(question.text.length).toBeGreaterThan(0)
      expect(question.topic.length).toBeGreaterThan(0)
      expect(question.text).not.toMatch(/get_|list_|compare_/)
    }
  })
})

describe('buildCapabilitySummary', () => {
  it('interpolates the live coverage period rather than a baked-in date range', () => {
    const summary = buildCapabilitySummary('Aug 1, 2026 through Aug 14, 2026')
    expect(summary).toContain('Aug 1, 2026 through Aug 14, 2026')
  })
})
