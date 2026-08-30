import { useEffect, useState } from 'react'

import { getJson } from '@/lib/api'

/**
 * Wire shape of GET /api/profile (backend `internal/httpapi/profile.go`) —
 * the same shape `ProfilePage` reads on mount. Kept as its own hook, mirroring
 * `Points/usePoints.ts`'s "every surface reads the SAME endpoint" discipline,
 * so any surface that wants to show the owner's saved restaurant identity
 * (initially: `Shell/Sidebar.tsx`) reads exactly what was actually saved,
 * never a locally-duplicated guess at the shape.
 */
export interface ProfileApi {
  name: string
  address: string
  phone: string
  email: string
  description: string
  photo: string | null
  updated_at: string
}

export interface ProfileState {
  data: ProfileApi | null
  error: string | null
}

type ProfileChangeListener = () => void

/**
 * Every currently-mounted `useProfile` caller's "refetch now" callback.
 *
 * `Sidebar` lives in the persistent `AppShell` and never remounts as the
 * owner navigates between routes, so a plain fetch-on-mount (this hook's
 * original shape, and still `usePoints`'s shape — see its own doc comment)
 * is not enough on its own: `Sidebar`'s one mount-time fetch is the only
 * chance it ever gets, so a save on `/profile` afterwards would sit there
 * unseen for the rest of the session (the QA "sidebar never updates"
 * finding). This keeps `usePoints`'s core discipline — no shared cache, no
 * context, every instance still reads `GET /api/profile` for itself — and
 * adds only the one thing a persistent, never-remounting consumer actually
 * needs: a way for a mutation elsewhere to say "your data is stale, fetch
 * again," same idea as `lib/chatStorage`'s subscribe-and-refresh listeners,
 * scoped to this one endpoint instead of localStorage.
 */
const listeners = new Set<ProfileChangeListener>()

/**
 * Tells every mounted `useProfile` caller to refetch `GET /api/profile`
 * right now. `ProfilePage` calls this after a successful `PUT` (so
 * `Sidebar` picks up the new name/photo without a reload) and again after a
 * 409 `profile_conflict` (so `Sidebar` picks up whatever the OTHER save
 * actually landed, rather than freezing on stale data while the error
 * message tells the owner someone else changed it).
 */
export function notifyProfileSaved(): void {
  listeners.forEach((listener) => listener())
}

/**
 * Fetch of the saved restaurant profile, refetched on mount and again
 * whenever `notifyProfileSaved` fires. `GET /api/profile` always returns a
 * well-formed (possibly all-empty) profile, even before the owner has ever
 * saved one — callers should treat an empty `name` as "nothing saved yet"
 * rather than an error.
 */
export function useProfile(): ProfileState {
  const [data, setData] = useState<ProfileApi | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false

    function load() {
      getJson<ProfileApi>('/api/profile')
        .then((response) => {
          if (!cancelled) {
            setData(response)
            setError(null)
          }
        })
        .catch((caught: unknown) => {
          if (!cancelled) {
            setError(caught instanceof Error ? caught.message : String(caught))
          }
        })
    }

    load()
    listeners.add(load)
    return () => {
      cancelled = true
      listeners.delete(load)
    }
  }, [])

  return { data, error }
}
