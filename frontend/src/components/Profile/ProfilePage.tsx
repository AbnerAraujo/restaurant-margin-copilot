import { useEffect, useRef, useState, type ChangeEvent, type FormEvent } from 'react'
import { AlertTriangle, CheckCircle2, ImageOff, Loader2, Store, Trash2 } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { ConfirmDialog } from '@/components/ui/confirm-dialog'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { PageContainer, PageHeader, Panel, PanelHeader } from '@/components/ui/page'
import { ApiError, getJson, putJson } from '@/lib/api'
import { explainRequestFailure, isNetworkFailure } from '@/lib/requestFailure'
import { useUnsavedChangesGuard } from '@/lib/useUnsavedChangesGuard'
import { notifyProfileSaved } from './useProfile'

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

/**
 * Mirrors backend/internal/httpapi/profile.go's profile*MaxLen constants —
 * a client-side `maxLength` on each field so a user discovers the real
 * cap by typing into it, rather than only after a round-trip 400 (QA
 * finding). The server enforces these independently and remains the
 * authority; this is purely a faster feedback loop.
 */
const PROFILE_FIELD_MAX_LENGTHS = {
  name: 200,
  address: 300,
  phone: 40,
  email: 254,
  description: 1000,
} as const

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

/** The form fields plus the photo, together — what "the last known-saved
 * state" means for the unsaved-changes guard below. */
interface ProfileSnapshot extends ProfileFormState {
  photo: string | null
}

const EMPTY_SNAPSHOT: ProfileSnapshot = { ...EMPTY_FORM, photo: null }

/**
 * This page's network-failure handling (QA found a blocked CORS preflight on
 * PUT /api/profile surfacing as the browser's bare "Failed to fetch") is now
 * `lib/requestFailure`, shared by every page — the fix that started here,
 * generalized, so no surface is left showing a raw Go error or a browser
 * string. Only SAVE failures take the "to save your changes" clause this
 * page's own wording had — the load path below uses the shared describer
 * directly, since telling someone a load failed "to save your changes" is
 * simply the wrong sentence.
 */
function saveErrorMessage(caught: unknown): string {
  if (isNetworkFailure(caught)) {
    return "We couldn't reach the server to save your changes. Check your connection and try again."
  }
  return explainRequestFailure(caught)
}

/**
 * Describes a photo already known to exceed `limitBytes`, for the "that
 * photo is over the limit" message — guaranteeing the displayed size never
 * reads as at-or-under the limit. Plain one-decimal rounding turns a file
 * exactly 1 byte over a whole-MB cap (e.g. 5,242,881 bytes against a 5MB
 * cap) into "5.0MB", which self-contradicts "...over the 5MB limit" (QA
 * finding). Ordinary oversized files still get the familiar "6.0MB" form;
 * only the boundary case falls back to an honest "just over" phrasing
 * rather than showing a misleadingly precise decimal.
 */
function describeOversizedPhoto(bytes: number, limitBytes: number): string {
  const megabytes = bytes / (1024 * 1024)
  const limitMegabytes = limitBytes / (1024 * 1024)
  const oneDecimal = megabytes.toFixed(1)
  if (parseFloat(oneDecimal) > limitMegabytes) {
    return `${oneDecimal}MB`
  }
  return `just over ${limitMegabytes}MB`
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

  // The last state this tab actually knows to be saved — either what GET
  // just loaded, or what the most recent successful PUT echoed back.
  // Compared against the live `form`/`photo` below to decide whether
  // there's real in-progress work a navigation would silently discard.
  const [savedSnapshot, setSavedSnapshot] = useState<ProfileSnapshot>(EMPTY_SNAPSHOT)

  const [submitting, setSubmitting] = useState(false)
  // Found live (same bug as Upload/UploadPage.tsx's committingRef and
  // Promotions/LogReplacementForm.tsx's submittingRef): a fast double-click
  // on "Save changes" fires two synchronous submit events before React's
  // re-render disables the button — setSubmitting(true) from the first is
  // batched, so the second, still-synchronous invocation reads the same
  // pre-update `submitting` and would double-PUT (both carrying the SAME
  // stale `updatedAt`, so the optimistic-concurrency 409 check below
  // couldn't even tell them apart from a genuine two-tab conflict). A ref
  // mutates synchronously, closing that window `disabled={submitting}`
  // cannot.
  const submittingRef = useRef(false)
  const [submitError, setSubmitError] = useState<string | null>(null)
  const [saved, setSaved] = useState(false)

  const fileInputRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    let cancelled = false
    getJson<ProfileApi>('/api/profile')
      .then((data) => {
        if (cancelled) return
        const loadedForm = {
          name: data.name,
          address: data.address,
          phone: data.phone,
          email: data.email,
          description: data.description,
        }
        setForm(loadedForm)
        setPhoto(data.photo)
        setUpdatedAt(data.updated_at)
        setSavedSnapshot({ ...loadedForm, photo: data.photo })
      })
      .catch((caught) => {
        if (!cancelled) setLoadError(explainRequestFailure(caught))
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
        `That photo is ${describeOversizedPhoto(file.size, MAX_PHOTO_BYTES)}, which is over the 5MB limit — choose a smaller image or compress it first.`,
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
      setSubmitError("Enter your restaurant's name — it's shown in the sidebar and can't be blank.")
      return
    }

    if (submittingRef.current) return
    submittingRef.current = true
    setSubmitting(true)
    try {
      const response = await putJson<ProfileApi>('/api/profile', {
        name,
        address: form.address.trim(),
        phone: form.phone.trim(),
        email: form.email.trim(),
        description: form.description.trim(),
        photo,
        // Optimistic concurrency: echo back exactly the updated_at this tab
        // last loaded. If another tab (or another save from this one) has
        // since changed the profile, the backend's updated_at will have
        // moved on and this PUT is refused with 409 rather than silently
        // reverting that other save (the QA two-tab lost-update finding).
        updated_at: updatedAt,
      })
      const savedForm = {
        name: response.name,
        address: response.address,
        phone: response.phone,
        email: response.email,
        description: response.description,
      }
      setForm(savedForm)
      setPhoto(response.photo)
      setUpdatedAt(response.updated_at)
      setSavedSnapshot({ ...savedForm, photo: response.photo })
      setSaved(true)
      // Sidebar (and any other surface reading `useProfile`) lives outside
      // this page and never remounts on its own — without this, a save here
      // would sit unseen for the rest of the session (QA finding).
      notifyProfileSaved()
    } catch (caught) {
      setSubmitError(saveErrorMessage(caught))
      if (caught instanceof ApiError && caught.status === 409) {
        // Someone else's save landed in between — refresh every other
        // surface reading the profile (the sidebar) to that real, current
        // value now, rather than leaving it frozen on what this tab loaded
        // while the error above tells the owner to reload. This form's own
        // fields are left alone: they're the owner's own attempted edit,
        // not something to silently discard.
        notifyProfileSaved()
      }
    } finally {
      submittingRef.current = false
      setSubmitting(false)
    }
  }

  // "Meaningful in-progress content" for this form: the live field/photo
  // values no longer match the last state actually known to be saved. A
  // freshly-loaded (or just-saved) form's `form`/`photo` and `savedSnapshot`
  // are identical, so this never fires on an untouched page — see
  // ProfilePage.test.tsx's "unsaved-changes guard" tests.
  const hasUnsavedChanges =
    form.name !== savedSnapshot.name ||
    form.address !== savedSnapshot.address ||
    form.phone !== savedSnapshot.phone ||
    form.email !== savedSnapshot.email ||
    form.description !== savedSnapshot.description ||
    photo !== savedSnapshot.photo

  const { isBlocked, confirmDiscard, cancelDiscard } = useUnsavedChangesGuard(hasUnsavedChanges)

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
        {/* `loadError` used to be read as a boolean here and its message
            thrown away, so an unreachable server and a failing query gave
            byte-identical copy — the one page whose load error said nothing
            about what actually went wrong. */}
        <p role="alert" className="flex items-start gap-1.5 text-sm text-destructive-text">
          <AlertTriangle className="mt-0.5 size-4 shrink-0" aria-hidden="true" />
          We couldn&apos;t load your profile. {loadError}
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
          Your name and photo appear in the sidebar on every page. Only the
          restaurant name is required.
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
                    Remove photo
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
                  maxLength={PROFILE_FIELD_MAX_LENGTHS.name}
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
                  maxLength={PROFILE_FIELD_MAX_LENGTHS.address}
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
                  maxLength={PROFILE_FIELD_MAX_LENGTHS.phone}
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
                  maxLength={PROFILE_FIELD_MAX_LENGTHS.email}
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
                  maxLength={PROFILE_FIELD_MAX_LENGTHS.description}
                />
              </div>
            </div>
          </div>

          <div className="flex flex-wrap items-center gap-3">
            <Button
              type="submit"
              disabled={submitting}
              onClick={() => {
                // Fires on every submit ATTEMPT, including one the
                // browser's native `type="email"`/`required` validation
                // goes on to block before `handleSubmit` ever runs. Without
                // this, a stale error from a previously-fixed field (e.g.
                // phone) stays on screen describing a field that's now
                // fine, while the actual blocker (e.g. email) shows no
                // error at all — the QA "stale field error" finding.
                setSubmitError(null)
                setSaved(false)
              }}
            >
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

      <ConfirmDialog
        open={isBlocked}
        title="Discard your profile changes?"
        description="The changes you've made to your restaurant's name, details, or photo haven't been saved yet. Leaving this page now discards them."
        confirmLabel="Discard changes"
        onConfirm={confirmDiscard}
        onCancel={cancelDiscard}
      />
    </PageContainer>
  )
}
