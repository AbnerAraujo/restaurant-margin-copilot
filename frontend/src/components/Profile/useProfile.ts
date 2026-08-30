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

/**
 * Single fetch of the saved restaurant profile. `GET /api/profile` always
 * returns a well-formed (possibly all-empty) profile, even before the owner
 * has ever saved one — callers should treat an empty `name` as "nothing
 * saved yet" rather than an error.
 */
export function useProfile(): ProfileState {
  const [data, setData] = useState<ProfileApi | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    getJson<ProfileApi>('/api/profile')
      .then((response) => {
        if (!cancelled) setData(response)
      })
      .catch((caught: unknown) => {
        if (!cancelled) {
          setError(caught instanceof Error ? caught.message : String(caught))
        }
      })
    return () => {
      cancelled = true
    }
  }, [])

  return { data, error }
}
