import {
  createContext,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from 'react'

/**
 * Theme activation for tokens that already existed. `index.css` has shipped
 * a complete `.dark` class — every surface, text, border, and semantic
 * token (`--success`, `--warning`, the four `--chart-*` slots) re-stepped
 * for a dark surface, not just inverted — since before this module existed,
 * and Tailwind is already wired for it (`@custom-variant dark (&:is(.dark
 * *))` in `index.css`; a handful of shadcn-derived primitives like
 * `button.tsx` already carry `dark:` classes). None of it was ever reachable:
 * nothing added `.dark` to the document, so it sat dead. This module is the
 * missing wiring, not a new palette — see `index.css` for the tokens
 * themselves.
 *
 * Three preferences, matching the `light` / `dark` / `system` triad
 * `docs/presentation.html` already established for the deck (its own
 * `:root` / `@media (prefers-color-scheme: dark)` / `[data-theme]` layers).
 * The mechanism differs on purpose: that deck is static HTML with no script
 * of its own, so it leans on CSS alone. This is a React app that already
 * expresses every token through Tailwind's `dark:` variant keyed off a
 * `.dark` class — so activation means toggling that class, driven by a tiny
 * bit of JS that also has to persist the user's choice and follow the OS
 * live when they pick "System". Same three-state rigor, the idiomatic
 * mechanism for this codebase rather than a second unrelated one.
 */

export type ThemePreference = 'light' | 'dark' | 'system'
type ResolvedTheme = 'light' | 'dark'

// Same `mbs.<domain>.v<n>` shape as chatStorage.ts's THREADS_KEY/PROMPTS_KEY.
// Versioned independently since this key's value (a bare preference string)
// will never share a schema with chat's JSON blobs.
export const THEME_STORAGE_KEY = 'mbs.theme.preference.v1'

const PREFERENCES: readonly ThemePreference[] = ['light', 'dark', 'system']

function isThemePreference(value: unknown): value is ThemePreference {
  return typeof value === 'string' && (PREFERENCES as readonly string[]).includes(value)
}

/**
 * Wrapped like every other localStorage access in this app (chatStorage.ts's
 * readJSON/writeJSON): Safari private mode throws on setItem, storage can be
 * full or disabled, and a hand-edited key must not crash the page. Falling
 * back to "system" degrades to the OS's own preference, never to a hard
 * failure.
 */
function readStoredPreference(): ThemePreference {
  try {
    const raw = window.localStorage.getItem(THEME_STORAGE_KEY)
    return isThemePreference(raw) ? raw : 'system'
  } catch {
    return 'system'
  }
}

function writeStoredPreference(preference: ThemePreference): void {
  try {
    window.localStorage.setItem(THEME_STORAGE_KEY, preference)
  } catch {
    // Losing the persisted preference is a convenience regression, not a
    // reason to interrupt whatever the user was doing when it happened.
  }
}

function prefersDarkFromSystem(): boolean {
  return (
    typeof window.matchMedia === 'function' &&
    window.matchMedia('(prefers-color-scheme: dark)').matches
  )
}

/** The one place that actually touches the DOM. */
function applyResolvedTheme(resolved: ResolvedTheme): void {
  document.documentElement.classList.toggle('dark', resolved === 'dark')
}

interface ThemeContextValue {
  /** The user's stored choice — what the Settings control reflects. */
  preference: ThemePreference
  /** What "system" actually resolved to, for copy like "System (dark)". */
  resolvedTheme: ResolvedTheme
  setPreference: (preference: ThemePreference) => void
}

const ThemeContext = createContext<ThemeContextValue | null>(null)

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [preference, setPreferenceState] = useState<ThemePreference>(readStoredPreference)
  // Tracked as its own bit of state, updated ONLY from the matchMedia
  // "change" event below — a legitimate external-system subscription, not a
  // value derived from other state. `resolvedTheme` itself is computed
  // straight from this plus `preference` on every render instead of being
  // synced via a second `useEffect`, which is what react-hooks'
  // set-state-in-effect rule flags: calling setState synchronously inside an
  // effect body (rather than from a subscription callback) causes an
  // avoidable extra render pass.
  const [systemPrefersDark, setSystemPrefersDark] = useState<boolean>(prefersDarkFromSystem)

  const resolvedTheme: ResolvedTheme =
    preference === 'system' ? (systemPrefersDark ? 'dark' : 'light') : preference

  // The one effect that actually touches the DOM — a genuine "synchronize
  // external system with React state" case, not a state update. Runs on
  // mount too: index.html's inline script (see its own comment) already set
  // the correct class before paint using the same storage key and the same
  // system-preference fallback, so this is a no-op re-application in the
  // common case, not the first time the class is set.
  useEffect(() => {
    applyResolvedTheme(resolvedTheme)
  }, [resolvedTheme])

  // Subscribed unconditionally rather than only while "system" is selected:
  // keeping `systemPrefersDark` current in the background means switching
  // *to* "system" later reflects the OS's current state immediately, with
  // no stale read. It only feeds into `resolvedTheme` above when
  // `preference === 'system'`, so it's inert the rest of the time.
  useEffect(() => {
    if (typeof window.matchMedia !== 'function') return
    const query = window.matchMedia('(prefers-color-scheme: dark)')
    function handleChange(event: MediaQueryListEvent) {
      setSystemPrefersDark(event.matches)
    }
    query.addEventListener('change', handleChange)
    return () => query.removeEventListener('change', handleChange)
  }, [])

  // Cross-tab sync. The browser only fires `storage` in OTHER tabs/windows
  // than the one that called `localStorage.setItem` — never the origin tab
  // — which is exactly the gap this closes: without it, a second open tab
  // kept showing its OLD preference (both applied and in the Settings
  // radiogroup's `aria-checked`, since that's driven straight off this
  // `preference` state) until it was manually reloaded, so two tabs of the
  // same single-owner app visibly disagreed about what was picked. Setting
  // `preference` here re-derives `resolvedTheme` and re-runs the
  // DOM-application effect above for free — no separate re-apply needed.
  useEffect(() => {
    function handleStorage(event: StorageEvent) {
      if (event.key !== THEME_STORAGE_KEY) return
      setPreferenceState(isThemePreference(event.newValue) ? event.newValue : 'system')
    }
    window.addEventListener('storage', handleStorage)
    return () => window.removeEventListener('storage', handleStorage)
  }, [])

  const value = useMemo<ThemeContextValue>(
    () => ({
      preference,
      resolvedTheme,
      setPreference: (next: ThemePreference) => {
        writeStoredPreference(next)
        setPreferenceState(next)
      },
    }),
    [preference, resolvedTheme],
  )

  return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>
}

export function useTheme(): ThemeContextValue {
  const context = useContext(ThemeContext)
  if (!context) {
    throw new Error('useTheme must be used within a ThemeProvider')
  }
  return context
}
