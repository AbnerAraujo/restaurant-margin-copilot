import { readFileSync, readdirSync } from 'node:fs'
import path from 'node:path'
import { describe, expect, it } from 'vitest'

import {
  ADVISORY_CAPABILITIES,
  BUSINESS_INSIGHT_KINDS,
  COMPUTED_CAPABILITIES,
  MCP_TOOL_NAMES,
  PRODUCT_CAPABILITIES,
  findAdvisoryCapability,
} from '@/capabilities'

/**
 * The contract test that makes `capabilities.ts` un-stale-able.
 *
 * This product has shipped a stale capability list twice — the Help page's
 * hardcoded "seven tools" and `exampleQuestions.ts`'s missing eighth entry —
 * and in both cases nothing failed. A shared TypeScript module alone would not
 * have caught either: every consumer would simply have agreed on the same
 * incomplete list. The only thing that can catch it is a check against the
 * code that actually defines the capability, which is Go.
 *
 * So these tests read the real backend source and assert, in BOTH directions,
 * that the frontend catalog and the Go registry name the same things. Add a
 * ninth MCP tool or a sixth insight kind without surfacing it, and
 * `npm test` goes red naming exactly what is missing.
 *
 * Reading Go source with a regex is deliberate, and its limits are the point:
 * this is a drift ALARM, not a parser. It has no opinion on tool schemas or
 * behavior — it only answers "does the frontend know about every capability
 * that exists, and only about capabilities that exist". That question is
 * exactly the one both previous staleness bugs got wrong.
 */

/** The repo root — vitest runs with `frontend/` as its cwd. */
const REPO_ROOT = path.resolve(process.cwd(), '..')
const MCP_TOOLS_DIR = path.join(REPO_ROOT, 'backend', 'internal', 'mcptools')
const ADVISOR_FILE = path.join(
  REPO_ROOT,
  'backend',
  'internal',
  'advisor',
  'advisor.go',
)
const TOOL_CONTRACTS_FILE = path.join(
  REPO_ROOT,
  'specs',
  '001-margin-reconciliation-qa',
  'contracts',
  'mcp-tools.md',
)
const README_FILE = path.join(REPO_ROOT, 'README.md')
const MCP_AND_SKILLS_FILE = path.join(REPO_ROOT, 'docs', 'mcp-and-skills.md')

/**
 * Every tool name actually registered with the MCP server, read from the
 * `mcp.NewTool("…")` calls in `internal/mcptools`. Test files are excluded:
 * a fake tool registered inside a `_test.go` fixture is not a product
 * capability.
 */
function registeredGoToolNames(): string[] {
  const names = new Set<string>()
  for (const file of readdirSync(MCP_TOOLS_DIR)) {
    if (!file.endsWith('.go') || file.endsWith('_test.go')) continue
    const source = readFileSync(path.join(MCP_TOOLS_DIR, file), 'utf8')
    for (const match of source.matchAll(/mcp\.NewTool\(\s*"([a-z0-9_]+)"/g)) {
      names.add(match[1])
    }
  }
  return [...names].sort()
}

/** Every insight kind constant declared in `internal/advisor/advisor.go`. */
function declaredGoInsightKinds(): string[] {
  const source = readFileSync(ADVISOR_FILE, 'utf8')
  const kinds = new Set<string>()
  for (const match of source.matchAll(/^\s*Kind\w+\s*=\s*"([a-z0-9_]+)"/gm)) {
    kinds.add(match[1])
  }
  return [...kinds].sort()
}

/** Every tool given its own `## \`name\`` section in the tool-contract doc. */
function documentedContractToolNames(): string[] {
  const source = readFileSync(TOOL_CONTRACTS_FILE, 'utf8')
  const names = new Set<string>()
  for (const match of source.matchAll(/^## `([a-z0-9_]+)`/gm)) {
    names.add(match[1])
  }
  return [...names].sort()
}

/**
 * Every tool named as the first cell of a `| \`name\` | ... |` row in
 * README.md's "## The N MCP tools..." table. Found in QA round 4: this
 * exact staleness class — a hand-maintained tool list drifting from the
 * real one — had already bitten the Help page and `exampleQuestions.ts`
 * (see this file's own doc comment above), and it had ALSO bitten this
 * prose README and `docs/mcp-and-skills.md` at the same time, undetected,
 * because neither is TypeScript this file previously read. Both are now in
 * scope.
 */
function readmeToolTableNames(): string[] {
  const source = readFileSync(README_FILE, 'utf8')
  const names = new Set<string>()
  for (const match of source.matchAll(/^\| `([a-z0-9_]+)` \|/gm)) {
    names.add(match[1])
  }
  return [...names].sort()
}

/**
 * Every tool named as the second cell of a `| # | \`name\` | ... |` row in
 * `docs/mcp-and-skills.md`'s "The exact N typed tools" table.
 */
function mcpAndSkillsToolTableNames(): string[] {
  const source = readFileSync(MCP_AND_SKILLS_FILE, 'utf8')
  const names = new Set<string>()
  for (const match of source.matchAll(/^\| \d+ \| `([a-z0-9_]+)` \|/gm)) {
    names.add(match[1])
  }
  return [...names].sort()
}

describe('capabilities catalog vs. the real backend', () => {
  it('names exactly the MCP tools registered in Go — no more, no fewer', () => {
    expect([...MCP_TOOL_NAMES].sort()).toEqual(registeredGoToolNames())
  })

  it('names exactly the MCP tools in the authoritative tool-contract doc', () => {
    expect([...MCP_TOOL_NAMES].sort()).toEqual(documentedContractToolNames())
  })

  it('names exactly the MCP tools in README.md\'s tool table', () => {
    expect([...MCP_TOOL_NAMES].sort()).toEqual(readmeToolTableNames())
  })

  it('names exactly the MCP tools in docs/mcp-and-skills.md\'s tool table', () => {
    expect([...MCP_TOOL_NAMES].sort()).toEqual(mcpAndSkillsToolTableNames())
  })

  it('names exactly the business-insight kinds declared in Go', () => {
    expect([...BUSINESS_INSIGHT_KINDS].sort()).toEqual(declaredGoInsightKinds())
  })
})

describe('capabilities catalog internal consistency', () => {
  it('gives every MCP tool exactly one computed capability', () => {
    const tools = COMPUTED_CAPABILITIES.map((c) => c.tool).sort()
    expect(tools).toEqual([...MCP_TOOL_NAMES].sort())
  })

  it('gives every insight kind exactly one advisory capability', () => {
    const kinds = ADVISORY_CAPABILITIES.map((c) => c.insightKind).sort()
    expect(kinds).toEqual([...BUSINESS_INSIGHT_KINDS].sort())
  })

  it('grounds every advisory capability in a real computed capability', () => {
    // The backend refuses advice whose posted tool_calls do not re-derive to
    // the claimed kind, so a dangling `groundedBy` would compose a request the
    // backend could never satisfy.
    const computedIds = new Set(COMPUTED_CAPABILITIES.map((c) => c.id))
    for (const advisory of ADVISORY_CAPABILITIES) {
      expect(computedIds).toContain(advisory.groundedBy)
    }
  })

  it('uses unique ids and human-readable copy throughout', () => {
    const ids = COMPUTED_CAPABILITIES.map((c) => c.id)
    expect(new Set(ids).size).toBe(ids.length)
    for (const capability of PRODUCT_CAPABILITIES) {
      expect(capability.label.length).toBeGreaterThan(0)
      expect(capability.description.length).toBeGreaterThan(0)
      // A raw tool name must never leak into owner-facing copy.
      expect(capability.label).not.toMatch(/_/)
      expect(capability.description).not.toMatch(/get_|list_|compare_/)
    }
  })

  it('resolves a known insight kind and rejects an unknown one', () => {
    expect(findAdvisoryCapability('high_commission')?.groundedBy).toBe(
      'platform_economics',
    )
    expect(findAdvisoryCapability('not_a_real_kind')).toBeUndefined()
  })
})
