import { Compass, Home } from 'lucide-react'
import { Link } from 'react-router-dom'

import { Button } from '@/components/ui/button'
import { Chip, PageContainer, PageHeader, Panel } from '@/components/ui/page'

/**
 * `*` — router.tsx's catch-all for any URL that doesn't match a real route
 * (a typo, a stale bookmark, an old link to a page that moved).
 *
 * Rendered as an ordinary routed page — a child of AppShell's `<Outlet>`,
 * same as every other route — rather than falling through to the root
 * `errorElement`. Before this route existed, an unmatched path had nowhere
 * to go but `RouteErrorBoundary`: the WHOLE app shell (sidebar included) was
 * replaced by the crash screen, the owner had no nav left to click back to
 * anywhere real, and the crash screen's own "Nothing else on the page is
 * affected" copy was false — everything was gone. Rendering inside the
 * shell keeps the sidebar/nav usable, which is the actual fix; this
 * component itself just supplies the page that belongs in that slot.
 *
 * Deliberately NOT wrapped in a class `ErrorBoundary`/reported to
 * `/api/client-errors` the way every other route's element is: a 404 is
 * expected user behavior, not an application fault, and reporting it there
 * would quietly turn "someone mistyped a URL" into false noise in the same
 * feed that exists to catch real crashes (see ErrorBoundary.tsx's own doc
 * comment on what that feed is for).
 */
export default function NotFoundPage() {
  return (
    <PageContainer className="flex flex-col gap-5">
      <PageHeader
        eyebrow="404"
        title="Page not found"
        meta={<Chip icon={Compass}>Nothing at this address</Chip>}
      />
      <Panel className="flex flex-col items-start gap-4 p-6 sm:p-8">
        <p className="max-w-prose-measure text-sm leading-relaxed text-muted-foreground">
          There&apos;s no page at this address. It may be an old link or a
          mistyped URL — everything else in the app is still right where you
          left it.
        </p>
        <div className="flex flex-wrap items-center gap-2">
          <Button asChild>
            <Link to="/">
              <Home aria-hidden="true" />
              Go to Home
            </Link>
          </Button>
          <Button asChild variant="outline">
            <Link to="/help">Visit Help</Link>
          </Button>
        </div>
      </Panel>
    </PageContainer>
  )
}
