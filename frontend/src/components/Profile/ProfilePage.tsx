import { useEffect, useRef, useState, type ChangeEvent, type FormEvent } from 'react'
import { AlertTriangle, CheckCircle2, ImageOff, Loader2, Store, Trash2 } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { PageContainer, PageHeader, Panel, PanelHeader } from '@/components/ui/page'
import { ApiError, getJson, putJson } from '@/lib/api'

// ---------------------------------------------------------------------------
// The restaurant owner's own company information and photo — a single-row
// settings-style resource (backend/migrations/000011_restaurant_profile),
// not a list of records. GET /api/profile always returns a well-formed
// profile, even before the owner has ever saved one, so this page has one
// steady shape to render into rather than a separate "nothing here yet"
// branch. PUT /api/profile is a full replace: the photo the owner didn't
// touch is resubmitted exactly as loaded, matching the backend's own
// full-replace contract (see internal/httpapi/profile.go's doc comment).
// ---------------------------------------------------------------------------

/** Mirrors backend/internal/httpapi/profile.go's allowedProfilePhotoTypes. */
const ALLOWED_PHOTO_TYPES = ['image/png', 'image/jpeg', 'image/webp']
/** Mirrors backend/internal/httpapi/profile.go's maxProfilePhotoBytes. */
const MAX_PHOTO_BYTES = 5 * 1024 * 1024

interface ProfileApi {
  name: string
  address: string
  phone: string
  email: string
  description: string
  photo: string | null
  updated_at: string
}

interface ProfileFormState {
  name: string
  address: string
  phone: string
  email: string
  description: string
}

const EMPTY_FORM: ProfileFormState = {
  name: '',
  address: '',
  phone: '',
  email: '',
  description: '',
}

// isNetworkFailure reports whether `caught` is the raw error `fetch()`
// itself throws when the request never reached a server at all — DNS
// failure, connection refused, or (as QA found for PUT /api/profile) a
// blocked CORS preflight. Browsers disagree on the wording ("Failed to
// fetch" in Chrome, "NetworkError when attempting to fetch resource" in
// Firefox, "Load failed" in Safari) and none of them explain why, so this
// checks the type fetch() actually throws (a TypeError, distinct from the
// `ApiError`/`Error` that getJson/putJson construct from a real HTTP
// response) rather than matching message text, which would silently stop
// working on a browser that phrases it differently.
function isNetworkFailure(caught: unknown): boolean {
  return caught instanceof TypeError
}

function errorMessage(caught: unknown): string {
  if (caught instanceof ApiError) return caught.message
  if (isNetworkFailure(caught)) {
    return "We couldn't reach the server to save your changes. Check your connection and try again."
  }
  if (caught instanceof Error) return caught.message
  return String(caught)
}

function formatMegabytes(bytes: number): string {
  return (bytes / (1024 * 1024)).toFixed(1)
}

/** Reads a File as a base64 data URI — the exact wire format PUT /api/profile expects. */
function readAsDataURL(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => resolve(reader.result as string)
    reader.onerror = () => reject(reader.error ?? new Error('could not read the file'))
    reader.readAsDataURL(file)
  })
}

/**
 * `/profile` — the restaurant owner's company information and photo. One
 * form, five text fields plus a photo, loaded once on mount and saved as a
 * full replace on submit.
 */
export default function ProfilePage() {
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState<string | null>(null)
  const [updatedAt, setUpdatedAt] = useState('')

  const [form, setForm] = useState<ProfileFormState>(EMPTY_FORM)
  const [photo, setPhoto] = useState<string | null>(null)
  const [photoError, setPhotoError] = useState<string | null>(null)

  const [submitting, setSubmitting] = useState(false)
  const [submitError, setSubmitError] = useState<string | null>(null)
  const [saved, setSaved] = useState(false)

  const fileInputRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    let cancelled = false
    getJson<ProfileApi>('/api/profile')
      .then((data) => {
        if (cancelled) return
        setForm({
          name: data.name,
          address: data.address,
          phone: data.phone,
          email: data.email,
          description: data.description,
        })
        setPhoto(data.photo)
        setUpdatedAt(data.updated_at)
      })
      .catch((caught) => {
        if (!cancelled) setLoadError(errorMessage(caught))
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [])

  function updateField<K extends keyof ProfileFormState>(field: K, value: string) {
    setForm((prev) => ({ ...prev, [field]: value }))
  }

  async function handlePhotoChange(event: ChangeEvent<HTMLInputElement>) {
    const file = event.target.files?.[0]
    if (!file) return

    if (!ALLOWED_PHOTO_TYPES.includes(file.type)) {
      setPhotoError('Photos must be PNG, JPEG, or WebP — choose a different file.')
      return
    }
    if (file.size > MAX_PHOTO_BYTES) {
      setPhotoError(
        `That photo is ${formatMegabytes(file.size)}MB, which is over the 5MB limit — choose a smaller image or compress it first.`,
      )
      return
    }

    try {
      const dataUri = await readAsDataURL(file)
      setPhoto(dataUri)
      setPhotoError(null)
    } catch {
      setPhotoError("We couldn't read that file — try choosing it again.")
    }
  }

  function removePhoto() {
    setPhoto(null)
    setPhotoError(null)
    if (fileInputRef.current) fileInputRef.current.value = ''
  }

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setSubmitError(null)
    setSaved(false)

    const name = form.name.trim()
    if (!name) {
      setSubmitError("Enter your restaurant's name — it's shown throughout the app and can't be blank.")
      return
    }

    setSubmitting(true)
    try {
      const response = await putJson<ProfileApi>('/api/profile', {
        name,
        address: form.address.trim(),
        phone: form.phone.trim(),
        email: form.email.trim(),
        description: form.description.trim(),
        photo,
      })
      setForm({
        name: response.name,
        address: response.address,
        phone: response.phone,
        email: response.email,
        description: response.description,
      })
      setPhoto(response.photo)
      setUpdatedAt(response.updated_at)
      setSaved(true)
    } catch (caught) {
      setSubmitError(errorMessage(caught))
    } finally {
      setSubmitting(false)
    }
  }

  if (loading) {
    return (
      <PageContainer className="flex flex-col gap-5">
        <PageHeader eyebrow="Company info" title="Profile" />
        <p className="flex items-center gap-1.5 text-sm text-muted-foreground">
          <Loader2 className="size-3.5 animate-spin" aria-hidden="true" />
          Loading your profile…
        </p>
      </PageContainer>
    )
  }

  if (loadError) {
    return (
      <PageContainer className="flex flex-col gap-5">
        <PageHeader eyebrow="Company info" title="Profile" />
        <p role="alert" className="flex items-start gap-1.5 text-sm text-destructive-text">
          <AlertTriangle className="mt-0.5 size-4 shrink-0" aria-hidden="true" />
          We couldn't load your profile. Try refreshing in a moment.
        </p>
      </PageContainer>
    )
  }

  return (
    <PageContainer className="flex flex-col gap-5">
      <PageHeader
        eyebrow="Company info"
        title="Profile"
        meta={
          updatedAt ? (
            <span className="text-xs text-muted-foreground">
              Last saved {new Date(updatedAt).toLocaleString()}
            </span>
          ) : null
        }
      />

      <Panel className="p-5 sm:p-6">
        <PanelHeader eyebrow="Restaurant details" title="Tell customers about your restaurant" />
        <p className="mt-1 text-xs text-muted-foreground">
          Shown throughout the app. Only the restaurant name is required.
        </p>

        <form
          onSubmit={(event) => {
            void handleSubmit(event)
          }}
          className="mt-4 flex flex-col gap-5"
        >
          <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:gap-5">
            <div className="flex shrink-0 flex-col items-start gap-2">
              <span className="text-xs font-medium text-foreground">Photo</span>
              <div className="flex size-28 items-center justify-center overflow-hidden rounded-lg border border-border bg-muted/40">
                {photo ? (
                  <img
                    src={photo}
                    alt="Restaurant photo preview"
                    className="size-full object-cover"
                  />
                ) : (
                  <Store className="size-8 text-muted-foreground" aria-hidden="true" />
                )}
              </div>
              <div className="flex items-center gap-2">
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  onClick={() => fileInputRef.current?.click()}
                >
                  {photo ? 'Change photo' : 'Choose photo'}
                </Button>
                {photo ? (
                  <Button type="button" variant="ghost" size="sm" onClick={removePhoto}>
                    <Trash2 aria-hidden="true" />
                    Remove
                  </Button>
                ) : null}
              </div>
              <input
                ref={fileInputRef}
                type="file"
                accept="image/png,image/jpeg,image/webp"
                className="sr-only"
                aria-label="Choose a restaurant photo"
                onChange={(event) => {
                  void handlePhotoChange(event)
                }}
              />
              <p className="text-micro text-muted-foreground">PNG, JPEG, or WebP, up to 5MB</p>
              {photoError ? (
                <p role="alert" className="flex items-start gap-1.5 text-xs text-destructive-text">
                  <ImageOff className="mt-0.5 size-3.5 shrink-0" aria-hidden="true" />
                  {photoError}
                </p>
              ) : null}
            </div>

            <div className="grid flex-1 grid-cols-1 gap-3 sm:grid-cols-2">
              <div className="flex flex-col gap-1.5 sm:col-span-2">
                <label htmlFor="profile-name" className="text-xs font-medium text-foreground">
                  Restaurant name
                </label>
                <Input
                  id="profile-name"
                  placeholder="e.g. Trattoria Bellavista"
                  value={form.name}
                  onChange={(event) => updateField('name', event.target.value)}
                  required
                  aria-required="true"
                />
              </div>

              <div className="flex flex-col gap-1.5 sm:col-span-2">
                <label htmlFor="profile-address" className="text-xs font-medium text-foreground">
                  Address
                </label>
                <Input
                  id="profile-address"
                  placeholder="123 Main St, Springfield"
                  value={form.address}
                  onChange={(event) => updateField('address', event.target.value)}
                />
              </div>

              <div className="flex flex-col gap-1.5">
                <label htmlFor="profile-phone" className="text-xs font-medium text-foreground">
                  Phone
                </label>
                <Input
                  id="profile-phone"
                  type="tel"
                  placeholder="+1 555 123 4567"
                  value={form.phone}
                  onChange={(event) => updateField('phone', event.target.value)}
                />
              </div>

              <div className="flex flex-col gap-1.5">
                <label htmlFor="profile-email" className="text-xs font-medium text-foreground">
                  Email
                </label>
                <Input
                  id="profile-email"
                  type="email"
                  placeholder="owner@yourrestaurant.com"
                  value={form.email}
                  onChange={(event) => updateField('email', event.target.value)}
                />
              </div>

              <div className="flex flex-col gap-1.5 sm:col-span-2">
                <label htmlFor="profile-description" className="text-xs font-medium text-foreground">
                  About
                </label>
                <Textarea
                  id="profile-description"
                  placeholder="Tell customers what makes your restaurant special"
                  value={form.description}
                  onChange={(event) => updateField('description', event.target.value)}
                />
              </div>
            </div>
          </div>

          <div className="flex flex-wrap items-center gap-3">
            <Button type="submit" disabled={submitting}>
              {submitting ? <Loader2 className="size-3.5 animate-spin" aria-hidden="true" /> : null}
              {submitting ? 'Saving…' : 'Save profile'}
            </Button>

            {submitError ? (
              <p role="alert" className="flex items-center gap-1.5 text-xs text-destructive-text">
                <AlertTriangle className="size-3.5 shrink-0" aria-hidden="true" />
                {submitError}
              </p>
            ) : null}

            {saved && !submitError ? (
              <p className="flex items-center gap-1.5 text-xs text-success-text">
                <CheckCircle2 className="size-3.5 shrink-0" aria-hidden="true" />
                Profile saved.
              </p>
            ) : null}
          </div>
        </form>
      </Panel>
    </PageContainer>
  )
}
