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

function resolveTheme(preference: ThemePreference): ResolvedTheme {
  if (preference === 'system') return prefersDarkFromSystem() ? 'dark' : 'light'
  return preference
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
  const [resolvedTheme, setResolvedTheme] = useState<ResolvedTheme>(() =>
    resolveTheme(preference),
  )

  // Applies on every preference change, including the very first render —
  // index.html's inline script (see its own comment) already set the
  // correct class before paint using the same storage key and the same
  // system-preference fallback, so this is a no-op re-application in the
  // common case, not the first time the class is set.
  useEffect(() => {
    const resolved = resolveTheme(preference)
    applyResolvedTheme(resolved)
    setResolvedTheme(resolved)
  }, [preference])

  // "System" must track the OS live: a user who opens the OS appearance
  // settings while this tab is open (or has it open across a scheduled
  // light/dark switch) should see the app follow without a reload. Only
  // subscribed while "system" is the active preference.
  useEffect(() => {
    if (preference !== 'system' || typeof window.matchMedia !== 'function') return
    const query = window.matchMedia('(prefers-color-scheme: dark)')
    function handleChange() {
      const resolved: ResolvedTheme = query.matches ? 'dark' : 'light'
      applyResolvedTheme(resolved)
      setResolvedTheme(resolved)
    }
    query.addEventListener('change', handleChange)
    return () => query.removeEventListener('change', handleChange)
  }, [preference])

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
