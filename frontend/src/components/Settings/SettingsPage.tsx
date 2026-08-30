import {
  BookOpen,
  Code2,
  ExternalLink,
  Hammer,
  Maximize,
  Minimize,
  Network,
  Presentation,
} from 'lucide-react'

import { Button } from '@/components/ui/button'
import { Chip, PageContainer, PageHeader, Panel, PanelHeader } from '@/components/ui/page'
import { useFullscreen } from '@/lib/useFullscreen'
import { ThemeToggle } from '@/components/Settings/ThemeToggle'

/**
 * `/settings` — deliberately small.
 *
 * This is a single-owner, single-tenant, no-auth prototype (CLAUDE.md's
 * non-goals; `docs/architecture.html`'s "Redundancy" section: "None. There
 * are no users, no sessions, no tenancy."). There is no server-side config
 * store and no `/api/settings` endpoint — nothing here posts anywhere. Every
 * control on this page is a real, working preference — Full screen (the
 * OS-level Fullscreen API) and Theme (light/dark/system, persisted to this
 * browser via `useTheme`/`ThemeProvider` in `@/lib/theme`, applied through
 * the `.dark`-class tokens `index.css` already defines for the whole
 * app) — or a link to a document that actually exists and is actually
 * hosted. The "Not built yet" panel at the
 * bottom names real, specifically-scoped future work — not a padded list of
 * plausible-sounding toggles — matching the disclosure pattern
 * `PointsCard.tsx`'s "Live" panel replaced (see that file's history: an
 * honest "not built" panel, swapped for real shipped content once the
 * feature it described actually existed). Nothing here pretends the
 * opposite is true yet.
 */
export default function SettingsPage() {
  const { isFullscreen, toggle } = useFullscreen()

  return (
    <PageContainer className="flex flex-col gap-5">
      <PageHeader
        eyebrow="My Business Steward"
        title="Settings"
        meta={
          <>
            <Chip>Single owner, no accounts</Chip>
            <Chip>Nothing here is stored server-side</Chip>
          </>
        }
      />

      <Panel aria-label="Display" className="p-5 sm:p-6">
        <PanelHeader eyebrow="This browser only" title="Display" />
        <div className="mt-4 flex flex-wrap items-center justify-between gap-3 border-t border-border pt-4">
          <div className="min-w-0">
            <p className="text-sm font-medium text-foreground">Full screen</p>
            <p className="mt-0.5 max-w-prose-measure text-xs leading-relaxed text-muted-foreground">
              Fill the viewport with no browser chrome — the same real,
              OS-level Fullscreen API as the toggle pinned to the top-right
              corner on every page, given a labeled home here too.
            </p>
          </div>
          <Button type="button" variant="outline" size="sm" onClick={toggle} aria-pressed={isFullscreen}>
            {isFullscreen ? (
              <Minimize className="size-3.5" aria-hidden="true" />
            ) : (
              <Maximize className="size-3.5" aria-hidden="true" />
            )}
            {isFullscreen ? 'Exit full screen' : 'Enter full screen'}
          </Button>
        </div>
        <div className="mt-4 flex flex-wrap items-center justify-between gap-3 border-t border-border pt-4">
          <div className="min-w-0">
            <p className="text-sm font-medium text-foreground">Theme</p>
            <p className="mt-0.5 max-w-prose-measure text-xs leading-relaxed text-muted-foreground">
              Light, dark, or match your device. Saved to this browser only,
              same as every other preference on this page.
            </p>
          </div>
          <ThemeToggle />
        </div>
      </Panel>

      <Panel aria-label="About this build" className="overflow-hidden">
        <div className="p-5 sm:p-6">
          <PanelHeader eyebrow="Reference" title="About this build" />
          <p className="mt-3 max-w-prose-measure text-sm leading-relaxed text-muted-foreground">
            My Business Steward is a take-home prototype: a Go reconciliation
            engine that owns every number, and a model layer that only
            explains what the engine already computed — never one that
            computes. These are the real, hosted documents that describe how
            it is built, linked here rather than re-explained.
          </p>
        </div>
        <ul className="divide-y divide-border border-t border-border">
          <SettingsLink
            icon={Code2}
            label="Source (GitHub)"
            description="Full repository — backend, frontend, dataset generator, specs."
            href="https://github.com/AbnerAraujo/restaurant-margin-copilot"
          />
          <SettingsLink
            icon={Presentation}
            label="Live presentation"
            description="26-slide deck walking through the product and the build."
            href="https://claude.ai/code/artifact/17a46fdf-c587-45c6-b1d6-904f1a03bc70"
          />
          <SettingsLink
            icon={Network}
            label="Live architecture diagram"
            description="Design system, reconciliation engine, and the full system end to end."
            href="https://claude.ai/code/artifact/dcda16f7-44d7-4160-8f72-d8593f432441"
          />
          <SettingsLink
            icon={BookOpen}
            label="Live API docs"
            description="Interactive Swagger UI covering every backend endpoint."
            href="https://claude.ai/code/artifact/6781bd96-bfa1-4fd7-821a-fe35cd3ac764"
          />
        </ul>
      </Panel>

      {/* Recessed surface + a distinct chip, matching the visual pattern
          `ui/page.tsx`'s `Panel` doc comment describes for "this is not
          built" content — never a disabled-looking fake control. */}
      <Panel tone="muted" aria-label="Not built yet" className="p-5 sm:p-6">
        <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
          <Chip icon={Hammer}>Not built</Chip>
          <h2 className="text-sm font-semibold tracking-tight text-foreground">
            Real future work, named — not stubbed
          </h2>
        </div>
        <p className="mt-2 max-w-prose-measure text-sm leading-relaxed text-muted-foreground">
          Nothing below exists in this codebase, and none of it is a config
          flag waiting to be flipped on. Per{' '}
          <span className="font-mono text-xs">docs/architecture.html</span>
          &apos;s &ldquo;What production would actually need&rdquo; section
          and the multi-tenant RFC (
          <span className="font-mono text-xs">docs/rfc-multi-tenant.md</span>
          , status: proposed, not approved for implementation):
        </p>
        <ul className="mt-3 list-disc space-y-1.5 pl-5 text-sm text-muted-foreground">
          <li>Accounts and authentication — today anyone who can reach the port can call every endpoint.</li>
          <li>Per-restaurant data isolation (multi-tenancy) — every table today assumes exactly one restaurant&apos;s data.</li>
          <li>Rate limiting on <span className="font-mono text-xs">/api/ask</span>, and a CORS policy read from configuration instead of a hard-coded localhost allow.</li>
          <li>Secrets from a secret manager rather than a shell profile.</li>
        </ul>
      </Panel>
    </PageContainer>
  )
}

function SettingsLink({
  icon: Icon,
  label,
  description,
  href,
}: {
  icon: typeof Code2
  label: string
  description: string
  href: string
}) {
  return (
    <li>
      <a
        href={href}
        target="_blank"
        rel="noreferrer"
        className="flex items-center gap-3 px-5 py-4 transition-colors hover:bg-accent focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50 sm:px-6"
      >
        <span className="flex size-9 shrink-0 items-center justify-center rounded-md bg-primary/10 text-primary">
          <Icon className="size-4" aria-hidden="true" />
        </span>
        <span className="min-w-0 flex-1">
          <span className="flex items-center gap-1.5 text-sm font-medium text-foreground">
            {label}
            <ExternalLink className="size-3 shrink-0 text-muted-foreground" aria-hidden="true" />
          </span>
          <span className="mt-0.5 block text-xs leading-relaxed text-muted-foreground">
            {description}
          </span>
        </span>
      </a>
    </li>
  )
}
